**Status:** Draft gabungan v1 dan v2. Belum ada yang dikerjakan. Rujukan kode (mis. `BasicCtr.java:108-132`, `rup/graph/`) belum diverifikasi ke repo; bagian yang bergantung pada fakta ditandai **[Perlu verifikasi]**.

## 1. Ringkasan

`audit_log` di SiRUP lama hanya mencatat "user A memanggil URL X". Tidak ada nilai sebelum/sesudah, tidak ada entitas, dan tidak diketahui apakah aksinya berhasil.

New SiRUP akan mencatat setiap perubahan sebagai **Audit Event** di tabel baru `audit_events`: siapa mengubah apa, pada entitas mana, kapan, dari nilai berapa ke berapa, dan apakah berhasil. Event sukses ditulis di transaksi bisnis yang sama, sehingga tidak ada perubahan tanpa jejak dan tidak ada jejak tanpa perubahan.

**Langkah pertama adalah Fase 0:** mengumpulkan fakta dan mengukur baseline sebelum skema dibuat, termasuk keputusan retensi dan data pribadi dari tim hukum/kepatuhan.

## 2. Kondisi Sekarang (Legacy)

Di SiRUP lama (Java Play Framework), audit dicatat otomatis oleh interceptor `@Before` pada controller induk (`BasicCtr.java:108-132`) setiap kali ada request POST.

| Kolom | Tipe | Isi |
| --- | --- | --- |
| `id` | BIGSERIAL / sequence | Auto-increment (`nextval('audit_log_seq')`) |
| `tipe` | VARCHAR(255) | Deskriptor aksi / URL endpoint, mis. `POST /rup/simpanDraftPaket` |
| `user_id` | INTEGER, nullable | ID akun dari sesi login |
| `username` | VARCHAR, nullable | Username / NIK / nama lengkap |
| `ip_address` | VARCHAR, nullable | IP pengguna (`remoteAddress` atau hop awal `X-Forwarded-For`) |
| `log_date` | TIMESTAMP | Waktu request diterima |
| `headers` | VARCHAR, nullable | Serialisasi mentah HTTP header |

**Kelemahan:**

1. **Tidak ada diff.** Bila PPK mengubah pagu dari Rp100 juta ke Rp500 juta, nilai lama dan baru tidak tersimpan.
2. **Tidak ada entitas.** Tidak ada kolom `paket_id`, `satker_id`, atau `tahun_anggaran`; riwayat paket harus dicari dengan `LIKE`.
3. **Tidak cocok dengan GraphQL.** Semua mutasi New SiRUP lewat `POST /graphql`, jadi kolom `tipe` tidak bermakna.
4. **Status eksekusi tidak diketahui.** Dicatat sebelum aksi berjalan, jadi sukses, ditolak guard, dan error tidak bisa dibedakan.
5. **Satu tabel tanpa partisi.** Backup dan index scan makin lambat seiring waktu.
6. **Header mentah** berisiko memuat token atau data sensitif.
7. **Penulisan sinkron** menambah latensi; besarnya belum pernah diukur.

## 3. Tujuan

Audit log New SiRUP harus bisa menjawab siapa, apa, entitas mana, kapan, dari nilai berapa ke berapa, dan apakah berhasil.

- **Prioritas utama:** kepatuhan dan pemeriksaan (auditor, LKPP, BPK).
- **Pendukung:** forensik keamanan dan Riwayat Perubahan Paket di UI.

## 4. Keputusan yang Disepakati

| # | Keputusan |
| --- | --- |
| 1 | Tujuan utama kepatuhan/pemeriksaan; pendukung forensik keamanan |
| 2 | Rilis pertama khusus service `rup`; kontrak event generik agar bisa dipakai modul lain |
| 3 | Pembaca: auditor/admin (pencarian, export) dan user (riwayat per paket) |
| 4 | Append-only: role aplikasi hanya punya `INSERT` |
| 5 | Event sukses ditulis sinkron di transaksi bisnis yang sama. Transactional outbox hanya bila pengukuran membuktikan perlu |
| 6 | Event gagal/ditolak guard dicatat untuk pengguna terautentikasi, ditulis di luar transaksi (best-effort). Request tanpa autentikasi hanya masuk log keamanan dengan rate limit |
| 7 | Satu Audit Event per aggregate root (paket); perubahan anak di-nest di diff. Mutasi multi-paket menghasilkan banyak event dengan `trace_id` sama |
| 8 | Diff ditangkap lewat `audit.Record(ctx, entity, before, after)` di service layer, ditegakkan lint/test |
| 9 | Allowlist field per `entity_type`; simpan `user_agent` saja, tanpa header mentah dan token |
| 10 | Tabel `audit_log` lama dibekukan (read-only), tanpa backfill dan tanpa view tiruan. Sisi baca menggabungkan keduanya dengan penanda `source=legacy` |
| 11 | Read API jadi fase tersendiri, dengan otorisasi berbasis satker/KLDI |
| 12 | Istilah baku: Audit Event, Audit Trail, Riwayat Perubahan Paket, Actor, Legacy Audit Log. Nama tabel `audit_events` |

## 5. Desain Target

### 5.1 Skema `audit_events`

| Kelompok | Kolom | Catatan |
| --- | --- | --- |
| Actor | `actor_id`, `actor_name`, `actor_role`, `ip_address`, `user_agent` | `actor_role` dan `id_satker` disimpan sebagai snapshot saat aksi |
| Impersonasi | `impersonated_by` | Hanya bila fitur impersonasi ada **[Perlu verifikasi]** |
| Entitas | `entity_type`, `entity_id`, `tahun_anggaran`, `kode_kldi`, `id_satker` | Rilis pertama: paket penyedia, paket swakelola, status/pengumuman |
| Aksi | `action`, `graphql_mutation`, `trace_id` | `action`: CREATE, UPDATE, DELETE, ANNOUNCE, REVISE |
| Perubahan | `changed_fields`, `old_values` (JSONB), `new_values` (JSONB), `schema_version` | Nama field tidak dimigrasi ulang; lapisan baca menerjemahkan versi lama |
| Hasil | `status`, `error_message`, `execution_time_ms`, `created_at` | `status`: SUCCESS, FAILED, REJECTED_GUARD |

### 5.2 Contoh Diff

```json
{
  "changed_fields": ["pagu", "metode_pemilihan"],
  "old": { "pagu": 100000000, "metode_pemilihan": "Pengadaan Langsung" },
  "new": { "pagu": 500000000, "metode_pemilihan": "Tender" }
}
```

### 5.3 Alur Penulisan

```mermaidjs
flowchart TD
  A["Request mutasi GraphQL"] --> B["Middleware GraphQL<br/>isi Actor, IP, user agent, mutasi, trace_id"]
  B --> C
  subgraph TX["Transaksi bisnis"]
    C["Service mengubah data paket"] --> D["audit.Record(ctx, entity, before, after)"]
  end
  D --> E{"Commit?"}
  E -- ya --> F["Event SUCCESS tersimpan<br/>bersama perubahan bisnis"]
  E -- "tidak: rollback / ditolak guard" --> G["Middleware tulis FAILED / REJECTED_GUARD<br/>di luar transaksi, best-effort"]
```

1. **Middleware GraphQL** mengisi audit context. Middleware tidak menulis event sukses karena tidak tahu data sebelum/sesudah.
2. **Service layer** memanggil `audit.Record(...)` di dalam transaksi bisnis; event ikut commit atau rollback.
3. **Jalur gagal:** bila transaksi rollback atau guard menolak, middleware menulis event di luar transaksi.
4. **Penegakan:** lint/test memastikan setiap mutation resolver memanggil `audit.Record` atau ditandai dikecualikan. Kelayakan lint di Go diuji di Fase 2.
5. **Pengecualian:** autosave draft tidak diaudit per simpan; hanya saat draft diumumkan atau dibuang **[Perlu verifikasi frekuensi `simpanDraftPaket`]**.

### 5.4 Partisi dan Indeks

Pola baca utama adalah "semua event untuk paket #X". Partisi per `created_at` membuat query itu memindai semua partisi bila tidak dibatasi waktu.

- **Default:** partisi per `tahun_anggaran`, bila paket terikat satu tahun anggaran.
- **Alternatif:** bila ada paket lintas tahun, partisi `created_at` dengan indeks `(entity_type, entity_id, created_at)` di tiap partisi.
- Keputusan ditunda sampai fakta paket multi-tahun jelas **[Perlu verifikasi]**.

```sql
CREATE TABLE audit_events (...) PARTITION BY LIST (tahun_anggaran);
```

### 5.5 Sisi Baca dan Otorisasi

- Riwayat Perubahan Paket untuk user, dibatasi satker/KLDI mereka.
- Pencarian dan export lintas satker untuk auditor/admin.
- Akses baca dicatat hanya untuk export dan pencarian lintas satker.
- Hasil baca menggabungkan `audit_events` dan Legacy Audit Log dengan penanda `source`.

### 5.6 Data Sensitif

- Allowlist field per `entity_type`; field di luar daftar tidak disimpan.
- Tidak menyimpan header mentah atau token.
- Konflik append-only dengan hak penghapusan data pribadi diputuskan tim hukum/kepatuhan sebelum Fase 1.

## 6. Fase Implementasi

```mermaidjs
flowchart LR
  F0["Fase 0<br/>Fakta & baseline"] --> F1["Fase 1<br/>Skema & migrasi"]
  F1 --> F2["Fase 2<br/>Audit context &<br/>jalur gagal"]
  F2 --> F3["Fase 3<br/>Capture diff"]
  F3 --> F4["Fase 4<br/>Mode bayangan &<br/>rekonsiliasi"]
  F4 --> F5["Fase 5<br/>Read API & Riwayat<br/>Perubahan Paket"]
  F5 --> F6["Fase 6<br/>Perluasan & arsip"]
```

| Fase | Lingkup | Hasil | Syarat mulai |
| --- | --- | --- | --- |
| 0. Fakta & baseline | Ukur biaya `INSERT audit_log` dan latensi mutasi; verifikasi paket multi-tahun, impersonasi, frekuensi `simpanDraftPaket`, daftar mutasi; keputusan retensi dari tim hukum | Dokumen fakta, angka baseline, keputusan retensi tertulis | — |
| 1. Skema & migrasi | DDL `audit_events` terpartisi, role append-only, tabel legacy dibekukan | Skema siap dan terisolasi | Fase 0 |
| 2. Audit context & jalur gagal | Middleware GraphQL mengisi context dan mencatat FAILED/REJECTED_GUARD | Konteks Actor tersedia di semua mutasi | Fase 1 |
| 3. Capture diff | `audit.Record` di service layer, allowlist, lint/test penegak | Event sukses dengan diff lengkap | Fase 2 |
| 4. Mode bayangan & rekonsiliasi | Event ditulis tapi belum ditampilkan; job harian membandingkan perubahan bisnis dengan event sukses | Selisih nol selama satu siklus | Fase 3 |
| 5. Read API & Riwayat Perubahan Paket | Query timeline, pencarian dan export auditor, otorisasi satker/KLDI | Fitur tampil ke user dan auditor | Fase 4 |
| 6. Perluasan & arsip | Tambah `program` dan `rka`; retensi dan arsip partisi; penggabungan `source=legacy` | Cakupan penuh | Fase 5 |

Bila Fase 0 menunjukkan penulisan sinkron melanggar target latensi, gunakan **transactional outbox** (tulis ke tabel outbox di transaksi yang sama, lalu relay ke Pub/Sub atau audit store) sebelum mempertimbangkan async murni.

## 7. Asumsi dan Pertanyaan Terbuka

| # | Pertanyaan | Default sementara |
| --- | --- | --- |
| 1 | Paket bisa berubah lintas tahun anggaran? | Tidak; partisi per `tahun_anggaran` |
| 2 | Ada impersonasi di New SiRUP? | Tidak; kolom `impersonated_by` tidak dibuat |
| 3 | Peran Actor disimpan snapshot atau dihitung saat baca? | Snapshot saat aksi |
| 4 | Bagaimana versi skema diff dikelola? | `schema_version` per event |
| 5 | Cakupan entitas rilis pertama? | Paket penyedia, paket swakelola, status/pengumuman; autosave draft dikecualikan |
| 6 | Verifikasi kelengkapan sebelum legacy dimatikan? | Mode bayangan satu siklus dengan rekonsiliasi harian |
| 7 | Retensi dan penghapusan data pribadi | **Belum diputuskan**; wajib sebelum Fase 1 |
| 8 | Perlu ADR untuk audit sinkron di transaksi? | Ya: sulit dibalik dan hasil trade-off nyata |

## 8. Risiko

| Risiko | Mitigasi |
| --- | --- |
| Latensi mutasi naik karena `INSERT` dan snapshot di transaksi yang sama | Ukur di Fase 0; fallback ke outbox |
| Mutasi baru lupa memanggil `audit.Record` | Lint/test penegak |
| Tabel membengkak oleh event bernilai rendah | Pengecualian autosave, partisi, kebijakan retensi |
| Kebocoran data sensitif lewat diff | Allowlist dan review field per entitas |
| Konflik append-only dengan hak data pribadi | Keputusan hukum sebelum Fase 1 |
| Rujukan kode belum diverifikasi | Fase 0 |

## 9. Kriteria Selesai

- [ ] Setiap mutasi pada entitas cakupan menghasilkan Audit Event dengan status benar.
- [ ] Rekonsiliasi harian menunjukkan selisih nol selama satu siklus penuh.
- [ ] Riwayat Perubahan Paket tampil di UI dan hanya memuat paket dalam otorisasi user.
- [ ] Role aplikasi tidak punya izin `UPDATE`/`DELETE` pada `audit_events`.
- [ ] Latensi mutasi dalam batas yang disepakati setelah baseline Fase 0.

## Lampiran: Perubahan dari v1

| Aspek | v1 | Versi ini | Alasan |
| --- | --- | --- | --- |
| Penulisan | Async buffer / Pub/Sub | Sinkron di transaksi yang sama | Async murni bisa kehilangan event saat crash |
| Event gagal | Disebut sebagai status, mekanisme tidak jelas | Jalur terpisah di luar transaksi | Rollback ikut menghapus event gagal bila satu transaksi |
| Capture diff | Middleware (Fase 2) dan helper (Fase 3) terpisah | Middleware mengisi context, `audit.Record` menulis | Middleware tidak tahu data sebelum/sesudah |
| Legacy | View/adapter ke `audit_log` | Bekukan tabel lama, gabung di sisi baca | Baris lama tidak punya entitas dan diff |
| Partisi | Per `created_at` | Per `tahun_anggaran` (default) | Pola baca utama per paket |
| Latensi | Target < 30 ms | Ukur baseline dulu | Angka belum punya dasar |
| Sisi baca | Tidak ada | Fase 5 tersendiri | Auditor dan UI butuh query dan otorisasi |
| Data sensitif | Tidak dibahas | Allowlist, tanpa header mentah | Header bisa memuat token |
| Fase awal | Mulai dari DDL | Mulai dari Fase 0 | Beberapa keputusan bergantung fakta yang belum dicek |
