# fingerku (ZKTeco REST API, Tailwind CSS v4 Dashboard & Pure Go Library)

`fingerku` adalah REST API Server mandiri (*pure Go*) dan Web Dashboard berbasis **Tailwind CSS v4** untuk berkomunikasi langsung dengan mesin absensi dan access control biometrik **ZKTeco / ZKSoftware** melalui protokol socket jaringan (TCP & UDP, default port `4370`), tanpa memerlukan SDK resmi Windows DLL/COM.

Dibangun menggunakan router **`go-chi/chi/v5`**, database lokal **SQLite (`fingerku.db`)**, frontend modern **Tailwind CSS v4**, dan fitur **Server-Sent Events (SSE)** untuk streaming presensi real-time.

---

## Fitur Utama

- 🎨 **Web Dashboard Tailwind CSS v4**: Antarmuka web modern, responsif, dark mode, dan glassmorphic untuk monitoring mesin, manajemen user, ekspor log, dan kontrol hardware.
- 🚀 **Pure Go & Ringan**: Menggunakan Go standard library, Chi router (`github.com/go-chi/chi/v5`), dan SQLite embedded driver (`modernc.org/sqlite`).
- 🌐 **RESTful API Lengkap**: Standar JSON API untuk integrasi dengan HRIS, Web App, Mobile App, maupun backend bahasa lain (PHP/Laravel, Node.js, Python, Java, .NET).
- ⚡ **Real-Time SSE Streaming**: Endpoint `/api/v1/events` untuk mendengarkan event absensi secara live saat pegawai menempelkan jari/kartu/wajah.
- 🗄️ **SQLite DB-First Integration**: Otomatis menyimpan dan melakukan sinkronisasi data user, template sidik jari, log presensi, dan riwayat audit.
- 👥 **Manajemen User & Biometrik**: CRUD User, manajemen hak akses (Admin/User), dan ekstraksi template sidik jari biometrik.
- 📊 **Log Presensi & Filter**: Query log presensi terfilter berdasarkan User ID, rentang tanggal, status masuk/keluar, dan pagination.
- 🛠️ **Kontrol Perangkat**: Sinkronisasi waktu RTC, buka relay kunci pintu (*door access control*), kontrol layar LCD, tes suara speaker, restart, dan shutdown.

---

## Struktur Proyek

```
fingerku/
├── go.mod
├── Makefile                    # Target build, serve, dev, test, clean
├── README.md                   # Dokumentasi REST API & Web Dashboard
├── api/                        # REST API Package (Chi Router & Handlers)
│   ├── server.go               # Server struct, SSE broker, middleware & routes
│   ├── handlers.go             # REST API endpoint handlers
│   ├── response.go             # Standard JSON response helpers
│   └── static/                 # Embedded Tailwind v4 frontend assets (index.html, style.css, app.js)
├── web/                        # Frontend Source (Tailwind CSS v4)
│   ├── package.json            # Node & Tailwind v4 CLI scripts
│   ├── src/input.css           # Tailwind v4 CSS input (@import "tailwindcss";)
│   └── static/                 # Output HTML, JS & compiled CSS
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
    └── examples/               # Contoh kode fungsional Go
```

---

## Menjalankan Mode Development & Production

### 1. Mode Fullstack Developer (Backend Go + Frontend Vite HMR)
```bash
# Menjalankan Backend API (Port 8080) dan Frontend Vite (Port 5173) secara bersamaan
make dev
```
- **Frontend Vite Live Reload (HMR)**: `http://localhost:5173` *(Realtime update setiap file diedit)*
- **Backend REST API**: `http://localhost:8080/api/v1`
- Tekan `Ctrl+C` untuk mematikan semua service secara bersamaan.

### 2. Mode Production Server (Embedded Single Binary)
```bash
# Kompilasi Tailwind CSS v4 & build executable mandiri
make build

# Menjalankan binary mandiri
./bin/fingerku-api --port 8080 --db fingerku.db
# atau:
make serve
```
Buka browser di **`http://localhost:8080`** untuk mengakses Web Dashboard.

---

## Dokumentasi REST API

Base URL: `http://localhost:8080`

### 1. Health & Status
- **`GET /health`**
  - Response: `{"success": true, "data": {"service": "fingerku-api", "status": "healthy", "time": "..."}}`
- **`GET /api/v1/status`**
  - Mengembalikan status koneksi mesin, uptime, dan statistik SQLite.

### 2. Konfigurasi Mesin (SQLite)
- **`GET /api/v1/config`**
  - Mengambil pengaturan mesin yang tersimpan di SQLite.
- **`PUT /api/v1/config`**
  - Memperbarui konfigurasi mesin di SQLite.

### 3. Pengguna & Biometrik Sidik Jari
- **`GET /api/v1/users`**
  - Mengambil seluruh pengguna terdaftar beserta jumlah sidik jarinya.
- **`GET /api/v1/users/{id}`**
  - Mengambil detail profil user beserta data template sidik jarinya.
- **`POST /api/v1/users`**
  - Mendaftarkan atau memperbarui pengguna di mesin dan SQLite.
- **`DELETE /api/v1/users/{id}`**
  - Menghapus user dan template sidik jarinya dari mesin dan SQLite.
- **`GET /api/v1/templates`**
  - Mengambil seluruh template sidik jari biometrik dari database.
- **`GET /api/v1/users/{id}/templates`**
  - Mengambil template sidik jari milik user tertentu.

### 4. Log Presensi (*Attendance Logs*)
- **`GET /api/v1/attendance`**
  - Query log presensi tersimpan di SQLite dengan pagination & filter (`?user_id=...&from=...&to=...&page=1&limit=50`).
- **`GET /api/v1/attendance/stats`**
  - Ringkasan statistik (total log, hadir hari ini, breakdown status).
- **`GET /api/v1/attendance/machine`**
  - Membaca log presensi langsung dari RAM mesin (*Read-Only*).

### 5. Sinkronisasi & Kontrol Hardware
- **`POST /api/v1/sync`**
  - Memicu sinkronisasi manual data user, template sidik jari, dan log presensi dari mesin ke SQLite.
- **`GET /api/v1/sync/history`**
  - Riwayat log sinkronisasi database.
- **`POST /api/v1/device/connect`**
  - Menghubungkan ke mesin fisik.
- **`POST /api/v1/device/disconnect`**
  - Memutuskan koneksi ke mesin.
- **`GET /api/v1/device/info`**
  - Informasi detail hardware, firmware, serial number, MAC, dan kapasitas memori.
- **`POST /api/v1/device/unlock`**
  - Memicu relay kunci pintu (*door access control*): `{"seconds": 5}`.
- **`POST /api/v1/device/synctime`**
  - Menyamakan jam RTC mesin dengan jam server.
- **`POST /api/v1/device/voice`**
  - Memutar prompt suara speaker: `{"index": 0}`.
- **`POST /api/v1/device/restart`**
  - Reboot mesin ZKTeco.
- **`POST /api/v1/device/poweroff`**
  - Matikan mesin ZKTeco.

### 6. Real-Time Event Streaming (SSE)
- **`GET /api/v1/events`**
  - Server-Sent Events (SSE) stream untuk tap biometrik real-time.

---

## Lisensi & Atribusi
Diadaptasi dan di-porting ke Go berdasarkan arsitektur protokol biner dari library `pyzk` oleh Fanani M. Ihsan dan kontributor.
