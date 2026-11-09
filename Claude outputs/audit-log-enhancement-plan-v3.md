**Status:** Draft v3, 9 Nov 2026. Menggantikan v1 dan v2 (Versi Claude). Fakta kode dicek ke backend-monorepo commit f8e5d53f29 (4 Nov 2026).

## 1. Ringkasan

New SiRUP akan mencatat setiap perubahan paket sebagai Audit Event di tabel baru `audit_events`: siapa, entitas mana, nilai lama dan baru, serta hasilnya. Event ditulis di transaksi bisnis yang sama. Tabel legacy `audit_log` tetap berjalan.

Plan ini menggabungkan v1 dan v2, lalu dicek ke repo backend-monorepo. Empat temuan mengubah plan:

1. **Service rup sudah menulis ke `audit_log`** di dua mutasi, dengan dua implementasi berbeda. Satu kemungkinan gagal diam-diam.
2. **`audit_log` tidak bisa dibekukan**, karena Java masih menulisnya dan auditor membacanya.
3. **Status gagal bisa ditangkap di satu titik**, yaitu paket `utils/graphresp`.
4. **Tidak ada impersonasi**, dan paket tahun jamak tetap satu baris per tahun anggaran.

**Perlu disetujui sebelum Fase 1:**

- Penulisan sinkron di transaksi bisnis
- Tetap menulis baris paritas ke `audit_log` selama Java masih hidup
- Kebijakan retensi dan data pribadi (tim hukum/kepatuhan)

## 2. Kondisi Sekarang

Tabel `public.audit_log` punya 7 kolom: id, tipe (varchar 255), user_id, username, ip_address, log_date, headers. Tabel ini dipakai bersama oleh Java dan service rup.

| Penulis | Kapan menulis | Masalah |
| --- | --- | --- |
| Java Play (interceptor `@Before`) | Setiap POST, sebelum aksi berjalan | Hasil aksi tidak diketahui |
| rup: `rupSoftDeleteConsolidation` | Setelah commit, best-effort | id tanpa sequence, tipe tidak dipotong, IP kosong |
| rup: `rupTriggerGenerateRka` | Setelah publish, dan saat ditolak guard | Tidak ada; dipakai sebagai acuan |

**Dugaan bug:** helper RKA menyebut `audit_log.id` tidak punya default, sedangkan helper soft delete tidak mengisi id. Bila RKA benar, setiap audit soft delete gagal dan hanya tercatat sebagai log WARN.

**Kelemahan desain legacy:**

1. Tidak ada nilai sebelum dan sesudah.
2. Tidak ada kolom entitas, jadi riwayat paket dicari dengan `LIKE`.
3. Semua mutasi New SiRUP lewat `POST /graphql`, jadi kolom tipe tidak bermakna.
4. Java mencatat sebelum aksi, jadi sukses dan gagal tidak bisa dibedakan.
5. Satu tabel tanpa partisi.
6. Header mentah bisa memuat token.
7. Biaya penulisan sinkron belum pernah diukur.

## 3. Tujuan dan Cakupan

Audit log harus bisa menjawab: siapa mengubah apa, pada entitas mana, kapan, dari nilai berapa ke berapa, dan apakah berhasil. Prioritas utamanya kepatuhan dan pemeriksaan (auditor, LKPP, BPK); pendukungnya forensik keamanan dan riwayat di UI.

**Rilis pertama: 26 dari 56 mutasi rup**, semuanya mengubah paket.

| Kelompok | Jumlah | Contoh mutasi |
| --- | --- | --- |
| Draft paket | 6 | `rupCreatePaketPenyedia`, `rupUpdatePaketSwakelola` |
| Revisi paket | 6 | `rupRevisePaketPenyedia`, `rupRevisiConsolidationSatuKeSatu` |
| Konsolidasi | 5 | `rupCreateConsolidation`, `rupSoftDeleteConsolidation` |
| Siklus paket RKA | 4 | `rupPublishPaketRka`, `rupUnfinalizePaketRka` |
| Inisiasi revisi | 4 | `rupApproveInisiasiRevisiPaket`, `rupRejectInisiasiRevisiPaket` |
| Transfer | 1 | `rupTransferPaket` |

- **Menyusul (Fase 6):** program, kegiatan, sub kegiatan, RKA, struktur anggaran, general, integrasi (28 mutasi).
- **Dikecualikan:** `rupSaveLogin` (masuk log keamanan) dan `rupGenerateUnduhPaket` (masuk log akses export).
- **Di luar cakupan:** perubahan aplikasi Java dan skema `audit_log`.

## 4. Keputusan Desain

Empat keputusan berubah setelah repo dicek; sembilan lainnya tetap dari v2.

**Berubah atau baru**

| Keputusan | Sebelumnya (v2) | Alasan |
| --- | --- | --- |
| Event gagal/ditolak ditangkap di `utils/graphresp`, ditulis di luar transaksi | Mekanisme belum jelas | Semua error klien lewat paket ini |
| Penegakan lewat registry test dan pengecekan runtime | Lint statis | Lint statis untuk resolver Go sulit dibuat |
| `audit_log` tetap ditulis untuk paritas lewat satu helper, sampai Java pensiun | Dibekukan | Java masih menulis, auditor masih membaca |
| Actor = id pengguna + persona id + snapshot role dan satker; tanpa `impersonated_by` | Kolom impersonasi opsional | Tidak ada impersonasi di kode |

**Tetap dari v2**

1. Tujuan utama kepatuhan; pendukung forensik keamanan.
2. Rilis pertama hanya service rup, dengan kontrak event generik.
3. Pembaca: auditor/admin dan user (riwayat per paket).
4. `audit_events` append-only: role aplikasi hanya `INSERT`.
5. Event sukses ditulis sinkron di transaksi bisnis yang sama; outbox hanya bila terbukti perlu.
6. Satu event per paket; mutasi multi-paket berbagi `trace_id`.
7. Allowlist field per entitas; hanya `user_agent`, tanpa header mentah dan token.
8. Read API jadi fase tersendiri dengan otorisasi satker/KLDI.
9. Istilah baku: Audit Event, Audit Trail, Riwayat Perubahan Paket, Actor, Legacy Audit Log.

## 5. Desain Target

### 5.1 Skema `audit_events`

Tabel baru di database yang sama dengan tabel paket, dibuat lewat migrasi rup berikutnya (`000010`).

| Kelompok | Kolom | Catatan |
| --- | --- | --- |
| Kunci | `id`, `tahun_anggaran` | id = UUID dari kode. PK `(id, tahun_anggaran)` karena wajib memuat kolom partisi |
| Actor | `actor_id_pengguna`, `actor_persona_id`, `actor_role`, `actor_id_satker`, `actor_kode_kldi`, `ip_address`, `user_agent` | Snapshot saat aksi |
| Entitas | `entity_type`, `entity_id`, `id_satker`, `kode_kldi` | Pemilik entitas, bukan pemilik akun |
| Aksi | `action`, `graphql_mutation`, `trace_id` | trace_id menghubungkan event dari satu mutasi |
| Perubahan | `changed_fields`, `old_values`, `new_values`, `schema_version` | Nilai dalam JSONB; nama field tidak dimigrasi ulang |
| Hasil | `status`, `error_code`, `error_message`, `execution_time_ms`, `created_at` | Lihat pemetaan status di bawah |

**Pemetaan status:** `failure.Forbidden`, `Invalid`, dan `NotFound` menjadi `REJECTED`. `failure.Internal` dan error tanpa tipe menjadi `FAILED`.

### 5.2 Alur Penulisan

Event sukses ikut transaksi bisnis. Event gagal dan baris paritas legacy ditulis sesudahnya, di luar transaksi.

```mermaidjs
flowchart TD
  A["Wrapper AroundOperations<br/>isi Actor, IP, user agent, trace_id"] --> B
  subgraph S["Service (domain paket)"]
    B["Guard: role, scope, validasi"] --> C
    subgraph T["Satu transaksi (InTx)"]
      C["Ubah tabel paket"] --> D["audit.Record ke audit_events"]
    end
  end
  D --> E{"Sukses?"}
  E -- ya --> F["Baris paritas audit_log<br/>setelah commit, best-effort"]
  E -- tidak --> G["utils/graphresp<br/>catat kode dan klasifikasi error"]
  G --> H["Wrapper tulis REJECTED/FAILED<br/>ke audit_events, di luar transaksi"]
  classDef accent fill:#e7f1fb,stroke:#2f6fd0,stroke-width:2px
  class D,H accent
```

1. **Wrapper** (`AroundOperations` di `server/routes.go`) mengisi audit context: Actor, IP, user agent, nama mutasi, trace_id.
2. **Service** memanggil `audit.Record(ctx, q, entity, before, after)` di dalam blok `InTx`.
3. **`utils/graphresp`** mencatat kode dan klasifikasi error ke audit context.
4. **Wrapper** menulis event `REJECTED`/`FAILED` setelah respons, untuk mutasi yang tidak sukses.
5. **Helper `utils/auditlog`** menulis baris paritas `audit_log` setelah commit.
6. **Pengecekan runtime:** mutasi sukses tanpa `audit.Record` memicu log ERROR dan metrik.

### 5.3 Partisi dan Indeks

- **Partisi per `tahun_anggaran`.** Paket Tahun Pembayaran adalah baris terpisah yang ditautkan ke Tahun Pertama, jadi setiap paket punya satu tahun.
- **Indeks per partisi:** `(entity_type, entity_id, created_at DESC)` untuk timeline; `(actor_id_pengguna, created_at DESC)` dan `(id_satker, created_at DESC)` untuk auditor.
- **Partisi tahun baru** dibuat sebelum tahun anggaran dibuka. Partisi DEFAULT dipantau agar tetap kosong.

### 5.4 Sisi Baca dan Otorisasi

- **User:** Riwayat Perubahan Paket dibatasi satker/KLDI caller.
- **Auditor/admin:** pencarian dan export lintas satker; setiap export ikut dicatat.
- **Data legacy:** baris Java ditampilkan dengan `source=legacy`. Baris paritas dari rup disaring agar tidak dobel.
- **BigQuery:** bila auditor membaca lewat BQ, `audit_events` ikut diproyeksikan.

### 5.5 Data Sensitif

- Allowlist field per entitas, ditulis di samping model dan diuji.
- Tidak menyimpan header mentah atau token.
- `error_message` hanya berisi pesan untuk klien; cause internal tetap di log.
- Konflik append-only dengan hak hapus data pribadi diputuskan tim hukum sebelum Fase 1.

## 6. Fase Implementasi

Tujuh fase berurutan. Fase berikutnya baru mulai setelah gerbang fase sebelumnya lolos.

| Fase | Lingkup | Gerbang keluar |
| --- | --- | --- |
| 0. Fakta dan baseline | Cek default `audit_log.id` dan log WARN audit yang gagal; ukur p95 latensi mutasi paket; hitung volume `audit_log`; cek proyeksi BigQuery; minta keputusan retensi | Dokumen fakta, angka baseline, keputusan retensi tertulis |
| 1. Skema dan perbaikan legacy | Migrasi `audit_events` terpartisi, grant `INSERT`-only, satukan dua helper `audit_log` | Migrasi lolos di staging; baris paritas tercatat |
| 2. Audit context dan jalur gagal | Wrapper, hook `utils/graphresp`, event `REJECTED`/`FAILED`, registry test mutasi | Mutasi baru tanpa entri registry membuat test gagal |
| 3. Capture diff | `audit.Record` di 26 mutasi paket, allowlist, pengecekan runtime | Test integrasi tiap mutasi menghasilkan event yang benar |
| 4. Mode bayangan | Event ditulis tapi belum ditampilkan; job harian mencocokkan `auditupdate` paket dengan event `SUCCESS` | Selisih nol satu siklus; latensi dalam batas |
| 5. Read API | Riwayat Perubahan Paket, pencarian dan export auditor, otorisasi satker/KLDI | UI hanya menampilkan paket dalam otorisasi user |
| 6. Perluasan dan arsip | 28 mutasi sisanya, kebijakan retensi, arsip partisi lama | Seluruh mutasi rup terdaftar di registry |

Bila penulisan sinkron melanggar batas latensi, gunakan **transactional outbox** sebelum mempertimbangkan async murni.

## 7. Fakta dari Repo dan Pertanyaan Terbuka

Enam pertanyaan v2 terjawab dari kode. Sisanya butuh akses DB, angka produksi, atau keputusan di luar engineering.

**Terjawab dari kode**

| Pertanyaan v2 | Jawaban | Sumber |
| --- | --- | --- |
| Paket bisa lintas tahun? | Tahun jamak ada, tapi tiap tahun disimpan sebagai paket terpisah | `create_penyedia_multiyears.go` |
| Ada impersonasi? | Tidak. `rupDelegasi` hanya memilih target delegasi peran | `delegasi.graphqls` |
| Ada autosave draft? | Tidak. Draft diubah lewat mutasi eksplisit, jadi ikut diaudit | `paket.graphqls` |
| Berapa mutasi yang ada? | 56 di 9 file skema; 26 di paket | `graph/*.graphqls` |
| Transaksi bisa dipakai bersama? | Ya, `RunInTx` mendukung nested join; 20 service paket memakainya | `utils/dbutil/tx.go` |
| Di mana status gagal ditangkap? | `utils/graphresp`, empat fungsi | `graphresp.go` |

**Preseden di monorepo**

- epic menulis ke tabel legacy bersama (`blob_log`) agar auditor punya satu permukaan.
- account punya `gql_audit_log`, tapi hanya mencatat nama operasi.
- helpdesk memakai kolom lama/baru per field.
- rup punya `reindex_year_audit` dengan status running/success/failed.

**Belum terjawab**

| # | Pertanyaan | Default sementara | Pemutus |
| --- | --- | --- | --- |
| 1 | Apakah `audit_log.id` punya default? | Pakai `nextval('audit_log_seq')` | Cek DB, Fase 0 |
| 2 | Retensi dan hak hapus data pribadi | Wajib diputuskan sebelum Fase 1 | Tim hukum/kepatuhan |
| 3 | DB SiRUP diproyeksikan ke BigQuery? | Anggap ya | Tim data/platform |
| 4 | Batas latensi mutasi | Setelah baseline Fase 0 | Tim rup |
| 5 | Dua mutasi dikecualikan? | Ya | Tim rup |
| 6 | Kapan paritas `audit_log` berhenti? | Saat Java SiRUP pensiun | Tim rup dan pemilik legacy |
| 7 | Perlu ADR untuk audit sinkron? | Ya | Tim rup |

## 8. Risiko

| Risiko | Mitigasi |
| --- | --- |
| Latensi mutasi naik | Ukur di Fase 0 dan 4; fallback ke outbox |
| Mutasi baru lupa diaudit | Registry test dan pengecekan runtime |
| Baris audit hilang diam-diam | Satu helper, log ERROR dan metrik, rekonsiliasi harian |
| Data sensitif bocor lewat diff | Allowlist dan review field per entitas |
| Tabel membengkak | Partisi per tahun dan kebijakan retensi |
| Append-only bertentangan dengan hak data pribadi | Keputusan hukum sebelum Fase 1 |
| Auditor membaca dua format tabel | Read API menggabungkan keduanya dengan penanda `source` |

## 9. Kriteria Selesai

- [ ] Setiap mutasi dalam cakupan menghasilkan Audit Event dengan status yang benar.
- [ ] Registry test gagal untuk mutasi baru yang belum terdaftar.
- [ ] Rekonsiliasi harian menunjukkan selisih nol selama satu siklus penuh.
- [ ] Riwayat Perubahan Paket hanya memuat paket dalam otorisasi user.
- [ ] Role aplikasi tidak punya izin `UPDATE`/`DELETE` pada `audit_events`.
- [ ] Baris paritas `audit_log` dari rup tercatat tanpa gagal tulis.
- [ ] Latensi mutasi dalam batas yang disepakati di Fase 0.

## Lampiran: Perubahan dari v1

| Aspek | v1 | v3 |
| --- | --- | --- |
| Penulisan | Async / Pub/Sub | Sinkron di transaksi yang sama |
| Event gagal | Hanya disebut sebagai status | Ditangkap di `utils/graphresp`, ditulis di luar transaksi |
| Capture diff | Middleware | Wrapper isi context, service menulis diff |
| Legacy | View ke `audit_log` | `audit_log` tetap ditulis untuk paritas |
| Partisi | Per `created_at` | Per `tahun_anggaran` |
| Latensi | Target < 30 ms | Ukur baseline dulu |
| Sisi baca | Tidak dibahas | Fase 5 tersendiri |
| Data sensitif | Tidak dibahas | Allowlist, tanpa header mentah |
| Fase awal | Langsung DDL | Fakta dan baseline dulu |
