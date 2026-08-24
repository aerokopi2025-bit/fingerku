# fingerku (Go ZKTeco Standalone Library & CLI)

`fingerku` adalah library dan CLI tool mandiri (*pure Go*) untuk berkomunikasi langsung dengan mesin absensi dan access control biometrik **ZKTeco / ZKSoftware** melalui protokol socket jaringan (TCP & UDP, default port `4370`), tanpa memerlukan SDK resmi Windows DLL/COM.

Proyek ini merupakan porting modern dari [pyzk](https://github.com/fananimi/pyzk) ke bahasa **Go (Golang)** dengan penambahan fitur konkurensi native (*Go channels, context cancellation, thread-safe client*).

---

## Fitur Utama

- 🚀 **Pure Go & Zero Heavy Dependencies**: Hanya menggunakan standard library Go (`net`, `encoding/binary`, `time`, `context`, `sync`).
- 🔄 **TCP & UDP Transport**: Mendukung protokol TCP (dengan framing header `0x5050`) dan UDP.
- ⚡ **Realtime Event Streaming (Live Capture)**: Menggunakan Go Channel (`<-chan zk.Attendance`) dan `context.Context` untuk mendengarkan event absensi secara seketika saat pegawai menempelkan jari/kartu/wajah.
- 👥 **Manajemen User Lengkap**: Ambil daftar pengguna, tambah/update pengguna baru, hapus pengguna, dan reset hak akses admin.
- 📊 **Log Presensi (Attendance Logs)**: Baca seluruh rekaman log absensi (Read-Only).
- 🧬 **Template Sidik Jari (Biometrics)**: Ekstraksi template sidik jari (`GetTemplates`), upload template per-user maupun high-rate batch (`SaveUserTemplate`), dan hapus template.
- 🛠️ **Kontrol Perangkat & Hardware**: Sinkronisasi waktu RTC, trigger relay kunci pintu (*door access control*), kontrol layar LCD, tes suara speaker, restart, dan shutdown.
- 💻 **Interactive CLI Utility**: Dilengkapi CLI `fingerku-cli` untuk kebutuhan diagnostik, backup, dan automasi terminal.
- 🌐 **Modern Web UI (Tailwind CSS v4)**: Antarmuka web modern (*Single-Page Application*) dengan Tailwind CSS v4, live monitoring kiosk presensi, manajemen user, ekspor log CSV/JSON, simulator demo mode, kontrol relay pintu, dan suara speaker.

---

## Struktur Proyek

```
fingerku/
├── go.mod
├── README.md
├── zk/                         # Core Package
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
├── web/                        # Web UI Frontend (Tailwind CSS v4)
│   ├── package.json            # Tailwind v4 scripts
│   ├── src/                    # Tailwind source (input.css)
│   └── static/                 # Embedded assets (index.html, style.css, app.js)
└── cmd/
    ├── fingerku-web/           # Standalone Web Server & UI Console
    │   ├── main.go
    │   ├── manager.go
    │   └── static/             # Embedded HTML/CSS/JS
    ├── fingerku-cli/           # CLI Application
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

### 1. Build Web UI Console (`fingerku-web`)
```bash
cd /home/rhee/Projects/go/fingerku
go build -o fingerku-web ./cmd/fingerku-web

# Jalankan server Web UI (Default port: 8080)
./fingerku-web --ip 192.168.1.201

# Jalankan dalam Mode Demo / Mock Simulator (Uji coba tanpa mesin fisik)
./fingerku-web --mock --port 8080
```
Buka browser di **`http://localhost:8080`**.

### 2. Build CLI Tool (`fingerku-cli`)
```bash
go build -o fingerku-cli ./cmd/fingerku-cli
```

### 3. Menjalankan Unit Tests
```bash
go test -v ./...
```

---

## Penggunaan CLI (`fingerku-cli`)

### 1. Sinkronisasi & Query Database SQLite
```bash
# Tarik seluruh log absensi & user dari mesin dan simpan ke database SQLite (dengan pencegahan duplikat)
./fingerku-cli sync-logs --ip 192.168.1.201 --db fingerku.db

# Tampilkan log absensi yang tersimpan di SQLite
./fingerku-cli db-logs --db fingerku.db --limit 50

# Filter log di SQLite berdasarkan User ID dan rentang tanggal
./fingerku-cli db-logs --db fingerku.db --user 1001 --from 2026-08-01 --to 2026-08-31

# Tampilkan ringkasan statistik absensi dari SQLite
./fingerku-cli db-stats --db fingerku.db
```

### 2. Perintah Kontrol & Diagnostik Mesin
```bash
# Menampilkan informasi perangkat & kapasitas memori
./fingerku-cli info --ip 192.168.1.201

# Mengambil daftar pengguna yang terdaftar
./fingerku-cli users --ip 192.168.1.201

# Mengambil seluruh log absensi langsung dari RAM mesin (Read-Only)
./fingerku-cli attendance --ip 192.168.1.201

# Monitoring absensi secara live & otomatis log ke database SQLite
./fingerku-cli live --ip 192.168.1.201 --db fingerku.db

# Membuka relay pintu selama 5 detik
./fingerku-cli unlock --ip 192.168.1.201 --seconds 5

# Sinkronisasi jam mesin dengan waktu komputer/server
./fingerku-cli synctime --ip 192.168.1.201

# Memutar suara voice prompt index 0 ("Thank you")
./fingerku-cli voice --ip 192.168.1.201 --index 0

# Reboot mesin
./fingerku-cli restart --ip 192.168.1.201
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
		zk.WithPassword(0), // isi jika mesin menggunakan commkey
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

### 4. Mendaftarkan User Baru

```go
newUser := zk.User{
	UID:       101,
	UserID:    "101",
	Name:      "Budi Santoso",
	Privilege: zk.UserDefault,
	Password:  "123456",
	GroupID:   "1",
	Card:      0,
}

if err := client.SetUser(newUser); err != nil {
	log.Fatalf("Gagal mendaftarkan user: %v", err)
}
fmt.Println("User berhasil didaftarkan!")
```

---

## Lisensi & Atribusi
Diadaptasi dan di-porting ke Go berdasarkan arsitektur protokol biner dari library `pyzk` oleh Fanani M. Ihsan dan kontributor.
