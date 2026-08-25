# fingerku (Go ZKTeco Standalone Library, CLI & REST API)

`fingerku` adalah library, CLI tool, dan REST API server mandiri (*pure Go*) untuk berkomunikasi langsung dengan mesin absensi dan access control biometrik **ZKTeco / ZKSoftware** melalui protokol socket jaringan (TCP & UDP, default port `4370`), tanpa memerlukan SDK resmi Windows DLL/COM.

Dilengkapi dengan penyimpanan lokal **SQLite (`fingerku.db`)**, REST API router berbasis **`go-chi/chi/v5`**, fitur **Live Capture Streaming (SSE)**, dan background service **Runner Daemon**.

---

## Fitur Utama

- 🚀 **Pure Go & Zero Heavy Dependencies**: Hanya menggunakan standard library Go, Chi router (`github.com/go-chi/chi/v5`), dan SQLite embedded driver (`modernc.org/sqlite`).
- 🌐 **Modern REST API with Chi**: RESTful API lengkap untuk integrasi dengan sistem HRIS, backend Node.js/PHP/Python/Java, atau frontend web/mobile.
- ⚡ **Server-Sent Events (SSE)**: Streaming event absensi secara live dan seketika via `/api/v1/events`.
- 🗄️ **SQLite DB-First Integration**: Otomatis membaca & menyimpan konfigurasi perangkat, data user, template sidik jari, audit history, dan riwayat presensi ke SQLite lokal.
- 👥 **Manajemen User & Biometrik**: Ambil daftar pengguna, template sidik jari, tambah/update pengguna baru, hapus pengguna, dan reset hak akses admin.
- 📊 **Log Presensi (Attendance Logs)**: Baca seluruh rekaman log absensi dari RAM mesin maupun query terfilter dari SQLite lokal.
- 🛠️ **Kontrol Hardware**: Sinkronisasi waktu RTC, trigger relay kunci pintu (*door access control*), kontrol layar LCD, tes suara speaker, restart, dan shutdown.
- 💻 **Interactive CLI Utility**: Dilengkapi CLI `fingerku-cli` untuk kebutuhan automasi dan operasional terminal.

---

## Struktur Proyek

```
fingerku/
├── go.mod
├── Makefile                    # Target build, run, serve, test, clean
├── README.md
├── api/                        # REST API Package (Chi Router & Handlers)
│   ├── server.go               # Server struct, SSE broker, middleware & routes
│   ├── handlers.go             # REST API endpoint handlers
│   └── response.go             # Standard JSON response helpers
├── storage/                    # SQLite Storage Layer
│   ├── models.go               # Struct database, DeviceConfig, AttendanceRecord
│   ├── db.go                   # SQLite schema, CRUD operations, WAL mode
│   └── db_test.go              # Storage unit tests
├── zk/                         # Core ZKTeco Protocol Package
│   ├── const.go                # Konstanta opcode, ACK, event flags
│   ├── errors.go               # Custom typed errors
│   ├── models.go               # Struct User, Attendance, Finger, Sizes, DeviceInfo
│   ├── protocol.go             # Checksum, commkey, framing TCP/UDP, time encode/decode
│   ├── client.go               # Client struct, options, connection handling
│   ├── device.go               # Info perangkat, relay, speaker, LCD, RTC
│   ├── user.go                 # CRUD User
│   ├── attendance.go           # Log presensi (Read-Only)
│   ├── finger.go               # Biometrik sidik jari
│   ├── buffer.go               # Chunking & transfer buffer besar
│   ├── live.go                 # Live capture streaming via Go channel
│   └── zk_test.go              # Unit tests
└── cmd/
    ├── fingerku-api/           # Standalone REST API Server Binary
    │   └── main.go
    ├── fingerku-cli/           # CLI Application & Background Runner
    │   └── main.go
    └── examples/               # Contoh kode fungsional
        ├── basic/              # Koneksi & info dasar
        ├── attendance/         # Mengambil log absensi
        ├── live/               # Real-time event monitor
        ├── users/              # Manajemen data user
        └── backup/             # Backup seluruh data ke JSON
```

---

## Instalasi & Build

```bash
# Build binary CLI dan API server ke folder bin/
make build

# Jalankan REST API Server (Chi) pada port 8080
make serve

# Jalankan background runner daemon (CLI Mode)
make dev

# Menjalankan unit tests
make test
```

---

## REST API Server (`go-chi/chi/v5`)

API Server dapat dijalankan menggunakan:
```bash
./bin/fingerku-api --port 8080
# atau menggunakan CLI:
./bin/fingerku-cli serve --api-port 8080
```

### Ringkasan Endpoint API:

| Method | Endpoint | Deskripsi |
|---|---|---|
| `GET` | `/health` | Health check service & uptime |
| `GET` | `/api/v1/status` | Status koneksi mesin, statistik SQLite & uptime |
| `GET` | `/api/v1/config` | Membaca konfigurasi perangkat dari SQLite |
| `PUT` | `/api/v1/config` | Memperbarui konfigurasi perangkat di SQLite |
| `POST` | `/api/v1/device/connect` | Menghubungkan ke mesin ZKTeco |
| `POST` | `/api/v1/device/disconnect` | Memutuskan koneksi dari mesin |
| `GET` | `/api/v1/device/info` | Informasi firmware, serial, MAC & kapasitas memori |
| `POST` | `/api/v1/device/unlock` | Buka relay pintu (*access control*) `{"seconds": 5}` |
| `POST` | `/api/v1/device/synctime` | Sinkronisasi jam RTC mesin dengan jam server |
| `POST` | `/api/v1/device/voice` | Putar prompt suara speaker `{"index": 0}` |
| `POST` | `/api/v1/device/restart` | Reboot mesin ZKTeco |
| `POST` | `/api/v1/device/poweroff` | Matikan mesin ZKTeco |
| `GET` | `/api/v1/users` | Daftar user terdaftar (beserta info role & jumlah sidik jari) |
| `POST` | `/api/v1/users` | Tambah atau update user di mesin & SQLite |
| `GET` | `/api/v1/users/{id}` | Detail user beserta template sidik jarinya |
| `DELETE` | `/api/v1/users/{id}` | Hapus user dari mesin dan SQLite |
| `GET` | `/api/v1/templates` | Daftar seluruh template sidik jari biometrik |
| `GET` | `/api/v1/users/{id}/templates` | Daftar template sidik jari untuk user tertentu |
| `GET` | `/api/v1/attendance` | Query log presensi SQLite (`?user_id=...&from=...&to=...&page=1&limit=50`) |
| `GET` | `/api/v1/attendance/stats` | Ringkasan statistik absensi (total, hari ini, breakdown status) |
| `GET` | `/api/v1/attendance/machine` | Membaca log presensi langsung dari RAM mesin (*Read-Only*) |
| `POST` | `/api/v1/sync` | Trigger manual sinkronisasi user, sidik jari, & log ke SQLite |
| `GET` | `/api/v1/sync/history` | Riwayat audit sinkronisasi |
| `GET` | `/api/v1/events` | Real-time Server-Sent Events (SSE) stream untuk tap biometrik |

---

## Penggunaan CLI (`fingerku-cli`)

### 1. Service & Konfigurasi (DB-First)
```bash
# Jalankan REST API Server
./bin/fingerku-cli serve --api-port 8080

# Jalankan background runner daemon
./bin/fingerku-cli run

# Lihat konfigurasi mesin yang tersimpan di database
./bin/fingerku-cli config

# Simpan / ubah konfigurasi default mesin ke database SQLite
./bin/fingerku-cli set-config --ip 192.168.1.201 --port 4370 --password 0
```

### 2. Sinkronisasi & Query Database SQLite
```bash
# Tarik seluruh user, template sidik jari biometrik & log absensi dari mesin dan simpan ke SQLite
./bin/fingerku-cli sync-logs

# Tampilkan daftar pengguna yang tersimpan di SQLite
./bin/fingerku-cli db-users

# Tampilkan template sidik jari biometrik yang tersimpan di SQLite
./bin/fingerku-cli db-templates

# Tampilkan log absensi yang tersimpan di SQLite
./bin/fingerku-cli db-logs --limit 50

# Filter log di SQLite berdasarkan User ID dan rentang tanggal
./bin/fingerku-cli db-logs --user 62593 --from 2026-08-01 --to 2026-08-31

# Tampilkan ringkasan statistik absensi dari SQLite
./bin/fingerku-cli db-stats
```

### 3. Perintah Kontrol & Diagnostik Mesin
```bash
# Menampilkan informasi perangkat, firmware, jaringan & kapasitas memori
./bin/fingerku-cli info

# Mengambil daftar pengguna & template sidik jari dari mesin (dan cache ke SQLite)
./bin/fingerku-cli users

# Mengambil template sidik jari biometrik dari mesin
./bin/fingerku-cli templates

# Mengambil seluruh log absensi langsung dari RAM mesin (Read-Only)
./bin/fingerku-cli attendance

# Monitoring absensi secara live & otomatis log ke database SQLite
./bin/fingerku-cli live

# Membuka relay pintu selama 5 detik
./bin/fingerku-cli unlock --seconds 5

# Sinkronisasi jam mesin dengan waktu komputer/server
./bin/fingerku-cli synctime

# Memutar suara voice prompt index 0 ("Thank you")
./bin/fingerku-cli voice --index 0

# Reboot mesin
./bin/fingerku-cli restart

# Matikan mesin
./bin/fingerku-cli poweroff
```

---

## Lisensi & Atribusi
Diadaptasi dan di-porting ke Go berdasarkan arsitektur protokol biner dari library `pyzk` oleh Fanani M. Ihsan dan kontributor.
