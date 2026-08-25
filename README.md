# fingerku (Go ZKTeco Standalone Library & CLI)

`fingerku` adalah library dan CLI tool mandiri (*pure Go*) untuk berkomunikasi langsung dengan mesin absensi dan access control biometrik **ZKTeco / ZKSoftware** melalui protokol socket jaringan (TCP & UDP, default port `4370`), tanpa memerlukan SDK resmi Windows DLL/COM.

Dilengkapi dengan penyimpanan lokal **SQLite (`fingerku.db`)**, fitur **Live Capture Streaming**, dan background service **Runner Daemon**.

---

## Fitur Utama

- 🚀 **Pure Go & Zero Heavy Dependencies**: Hanya menggunakan standard library Go dan SQLite embedded driver (`modernc.org/sqlite`).
- 🔄 **TCP & UDP Transport**: Mendukung protokol TCP (dengan framing header `0x5050`) dan UDP.
- 🗄️ **SQLite DB-First Integration**: Otomatis membaca & menyimpan konfigurasi perangkat, data user, audit history, dan riwayat presensi ke SQLite lokal.
- ⚡ **Realtime Event Streaming (Live Capture)**: Menggunakan Go Channel (`<-chan zk.Attendance`) dan `context.Context` untuk mendengarkan event absensi secara seketika saat pegawai menempelkan jari/kartu/wajah.
- 👥 **Manajemen User Lengkap**: Ambil daftar pengguna, tambah/update pengguna baru, hapus pengguna, dan reset hak akses admin.
- 📊 **Log Presensi (Attendance Logs)**: Baca seluruh rekaman log absensi dari RAM mesin maupun query dari SQLite lokal.
- 🧬 **Template Sidik Jari (Biometrics)**: Ekstraksi template sidik jari (`GetTemplates`), upload template per-user (`SaveUserTemplate`), dan hapus template.
- 🛠️ **Kontrol Perangkat & Hardware**: Sinkronisasi waktu RTC, trigger relay kunci pintu (*door access control*), kontrol layar LCD, tes suara speaker, restart, dan shutdown.
- 💻 **Interactive CLI & Background Service**: Dilengkapi CLI `fingerku-cli` untuk kebutuhan automasi, sync service, dan query database.

---

## Struktur Proyek

```
fingerku/
├── go.mod
├── Makefile                    # Target build, run, test, clean
├── README.md
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

### 1. Build Binary dengan Make
```bash
# Build binary CLI ke bin/fingerku-cli
make build

# Menjalankan seluruh unit tests
make test

# Menjalankan background service runner
make dev
```

---

## Penggunaan CLI (`fingerku-cli`)

Secara default, seluruh perintah CLI menggunakan konfigurasi target mesin yang tersimpan di **SQLite (`fingerku.db`)**. Jika flag `--ip` atau `--port` tidak disertakan, CLI akan otomatis membaca pengaturan yang tersimpan di database.

### 1. Service Daemon & Konfigurasi (DB-First)
```bash
# Jalankan background runner daemon (otomatis sync user/log & mendengarkan live punch)
./bin/fingerku-cli run

# Jalankan runner dengan auto-sync berkala (misal setiap 60 detik)
./bin/fingerku-cli run --auto-sync-interval 60

# Lihat konfigurasi mesin yang saat ini tersimpan di database
./bin/fingerku-cli config

# Simpan / ubah konfigurasi default mesin ke database SQLite
./bin/fingerku-cli set-config --ip 192.168.1.201 --port 4370 --password 0
```

### 2. Sinkronisasi & Query Database SQLite
```bash
# Tarik seluruh user, template sidik jari biometrik & log absensi dari mesin dan simpan ke SQLite
./bin/fingerku-cli sync-logs

# Tampilkan daftar pengguna yang tersimpan di SQLite (beserta jumlah sidik jari)
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

# Mengambil template sidik jari biometrik dari mesin (dan cache ke SQLite)
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

## Contoh Penggunaan Go Library

### 1. Inisialisasi & Koneksi

```go
package main

import (
	"fmt"
	"log"
	"time"

	"fingerku/zk"
)

func main() {
	client := zk.New("192.168.1.201",
		zk.WithPort(4370),
		zk.WithTimeout(10*time.Second),
		zk.WithPassword(0), // isi jika mesin menggunakan commkey password
	)

	if err := client.Connect(); err != nil {
		log.Fatalf("Gagal konek: %v", err)
	}
	defer client.Disconnect()

	fmt.Println("Berhasil terhubung ke mesin ZKTeco!")
}
```

### 2. Mengambil Data User & Log Presensi

```go
// Kunci perangkat sementara saat membaca data
_ = client.DisableDevice()
defer client.EnableDevice()

// Ambil daftar user
users, err := client.GetUsers()
if err == nil {
	for _, u := range users {
		fmt.Printf("UID: %d | User ID: %s | Nama: %s | Role: %s\n",
			u.UID, u.UserID, u.Name, u.PrivilegeName())
	}
}

// Ambil log absensi
records, err := client.GetAttendance()
if err == nil {
	for _, r := range records {
		fmt.Printf("[%s] User: %s | Status: %s (Punch: %d)\n",
			r.Timestamp.Format("2006-01-02 15:04:05"), r.UserID, r.StatusName(), r.Punch)
	}
}
```

### 3. Monitoring Absensi Real-Time (Live Capture)

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

events, errs := client.LiveCapture(ctx)

for {
	select {
	case err := <-errs:
		if err != nil {
			log.Printf("Error: %v", err)
		}
		return
	case ev, ok := <-events:
		if !ok {
			return
		}
		fmt.Printf("👉 Absensi Masuk: User ID %s pada %s (%s)\n",
			ev.UserID, ev.Timestamp.Format("15:04:05"), ev.StatusName())
	}
}
```

---

## Lisensi & Atribusi
Diadaptasi dan di-porting ke Go berdasarkan arsitektur protokol biner dari library `pyzk` oleh Fanani M. Ihsan dan kontributor.
