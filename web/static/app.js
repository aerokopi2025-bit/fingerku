// Fingerku Web UI - Application Logic & State Manager with SQLite Database
const App = {
    state: {
        activeTab: 'dashboard',
        connected: false,
        config: { ip: '192.168.1.201', port: 4370, password: 0, udp: false, omit_ping: false, mock: false },
        info: null,
        users: [],
        attendance: [],
        dbAttendance: [],
        dbTotalRecords: 0,
        dbStats: { total_records: 0, total_users: 0, today_records: 0, today_unique_users: 0, status_counts: {} },
        dataSource: 'sqlite', // 'sqlite' or 'device'
        templates: [],
        recentPunches: [],
        lastPunch: null,
        uptimeSeconds: 0,
        audioEnabled: true,
        darkMode: true,
        loading: {
            users: false,
            attendance: false,
            templates: false,
            connecting: false,
            syncing: false,
            action: false
        },
        userFilter: {
            search: '',
            role: 'all'
        },
        attFilter: {
            search: '',
            status: 'all',
            startDate: '',
            endDate: '',
            page: 1,
            pageSize: 15
        },
        logs: [],
        editingUser: null,
        modals: {
            connect: false,
            userForm: false,
            deleteUser: false,
            clearAdmin: false,
            reboot: false,
            shutdown: false,
            deleteTemplate: false
        },
        selectedUserForDelete: null,
        selectedTemplateForDelete: null,
        unlockDuration: 3,
        lcdInput: { line: 1, text: 'Fingerku Ready' },
        voiceIndex: 0
    },

    sse: null,
    audioCtx: null,

    init() {
        // Load settings from localStorage
        const savedTheme = localStorage.getItem('fingerku_theme');
        if (savedTheme) {
            this.state.darkMode = savedTheme === 'dark';
        }
        this.applyTheme();

        const savedAudio = localStorage.getItem('fingerku_audio');
        if (savedAudio !== null) {
            this.state.audioEnabled = savedAudio === 'true';
        }

        const savedSource = localStorage.getItem('fingerku_datasource');
        if (savedSource) {
            this.state.dataSource = savedSource;
        }

        // Setup real-time clock
        this.startClock();

        // Initial connection check & load data
        this.checkStatus().then(() => {
            this.connectSSE();
            this.loadDBStats();
            this.loadDBAttendance();
        });

        // Periodic status polling (every 10s)
        setInterval(() => {
            if (this.state.connected) {
                this.refreshStatusOnly();
            }
            this.loadDBStats();
        }, 10000);

        this.addLog('Sistem Web UI & Database SQLite siap', 'info');
    },

    // ----------------------------------------------------
    // Theme & Audio Helpers
    // ----------------------------------------------------
    toggleTheme() {
        this.state.darkMode = !this.state.darkMode;
        localStorage.setItem('fingerku_theme', this.state.darkMode ? 'dark' : 'light');
        this.applyTheme();
    },

    applyTheme() {
        if (this.state.darkMode) {
            document.documentElement.classList.add('dark');
        } else {
            document.documentElement.classList.remove('dark');
        }
    },

    toggleAudio() {
        this.state.audioEnabled = !this.state.audioEnabled;
        localStorage.setItem('fingerku_audio', this.state.audioEnabled);
        this.showToast('info', 'Audio', this.state.audioEnabled ? 'Suara notifikasi aktif' : 'Suara notifikasi dinonaktifkan');
    },

    setDataSource(source) {
        this.state.dataSource = source;
        localStorage.setItem('fingerku_datasource', source);
        this.state.attFilter.page = 1;
        if (source === 'sqlite') {
            this.loadDBAttendance();
            this.loadDBStats();
        } else {
            this.loadAttendance();
        }
        this.render();
    },

    playPunchChime(status = 0) {
        if (!this.state.audioEnabled) return;
        try {
            if (!this.audioCtx) {
                this.audioCtx = new (window.AudioContext || window.webkitAudioContext)();
            }
            if (this.audioCtx.state === 'suspended') {
                this.audioCtx.resume();
            }

            const now = this.audioCtx.currentTime;
            const osc = this.audioCtx.createOscillator();
            const gain = this.audioCtx.createGain();

            osc.connect(gain);
            gain.connect(this.audioCtx.destination);

            if (status === 0) {
                // Check-in: Pleasant high chime
                osc.frequency.setValueAtTime(587.33, now); // D5
                osc.frequency.setValueAtTime(880.00, now + 0.1); // A5
                gain.gain.setValueAtTime(0.2, now);
                gain.gain.exponentialRampToValueAtTime(0.001, now + 0.35);
                osc.start(now);
                osc.stop(now + 0.35);
            } else {
                // Check-out / others: Melodic chime
                osc.frequency.setValueAtTime(659.25, now); // E5
                osc.frequency.setValueAtTime(523.25, now + 0.1); // C5
                gain.gain.setValueAtTime(0.2, now);
                gain.gain.exponentialRampToValueAtTime(0.001, now + 0.35);
                osc.start(now);
                osc.stop(now + 0.35);
            }
        } catch (e) {
            console.error('Audio playback error:', e);
        }
    },

    startClock() {
        const updateClock = () => {
            const clockEl = document.getElementById('digital-clock');
            const dateEl = document.getElementById('digital-date');
            const kioskClock = document.getElementById('kiosk-clock');
            const kioskDate = document.getElementById('kiosk-date');

            const now = new Date();
            const timeStr = now.toTimeString().split(' ')[0];
            const dateStr = now.toLocaleDateString('id-ID', { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' });

            if (clockEl) clockEl.textContent = timeStr;
            if (dateEl) dateEl.textContent = dateStr;
            if (kioskClock) kioskClock.textContent = timeStr;
            if (kioskDate) kioskDate.textContent = dateStr;

            if (this.state.connected) {
                this.state.uptimeSeconds++;
            }
        };
        updateClock();
        setInterval(updateClock, 1000);
    },

    // ----------------------------------------------------
    // Navigation & Tabs
    // ----------------------------------------------------
    setTab(tab) {
        this.state.activeTab = tab;
        this.render();

        if (tab === 'users' && this.state.users.length === 0 && this.state.connected) {
            this.loadUsers();
        } else if (tab === 'attendance') {
            if (this.state.dataSource === 'sqlite') {
                this.loadDBAttendance();
                this.loadDBStats();
            } else if (this.state.attendance.length === 0 && this.state.connected) {
                this.loadAttendance();
            }
        } else if (tab === 'templates' && this.state.templates.length === 0 && this.state.connected) {
            this.loadTemplates();
        }
    },

    // ----------------------------------------------------
    // Server-Sent Events (SSE)
    // ----------------------------------------------------
    connectSSE() {
        if (this.sse) {
            this.sse.close();
        }

        this.sse = new EventSource('/api/events');

        this.sse.addEventListener('connected', (e) => {
            this.addLog('Koneksi streaming SSE terhubung', 'success');
        });

        this.sse.addEventListener('punch', (e) => {
            try {
                const punch = JSON.parse(e.data);
                this.handleIncomingPunch(punch);
            } catch (err) {
                console.error('Failed to parse punch event:', err);
            }
        });

        this.sse.addEventListener('sync', (e) => {
            try {
                const syncData = JSON.parse(e.data);
                this.addLog(`Sinkronisasi SQLite berhasil: +${syncData.new_inserted} baru, ${syncData.skipped} duplikat`, 'success');
                this.loadDBStats();
                if (this.state.dataSource === 'sqlite') {
                    this.loadDBAttendance();
                }
            } catch (err) {}
        });

        this.sse.addEventListener('status', (e) => {
            try {
                const status = JSON.parse(e.data);
                this.state.connected = !!status.connected;
                this.addLog(`Status perangkat berubah: ${this.state.connected ? 'Terhubung' : 'Terputus'}`, 'info');
                this.checkStatus();
            } catch (err) {}
        });

        this.sse.addEventListener('log', (e) => {
            this.addLog(e.data, 'warning');
        });

        this.sse.onerror = () => {
            setTimeout(() => {
                if (this.sse && this.sse.readyState === EventSource.CLOSED) {
                    this.connectSSE();
                }
            }, 3000);
        };
    },

    handleIncomingPunch(punch) {
        punch.dateObj = new Date(punch.timestamp);
        punch.formattedTime = punch.dateObj.toTimeString().split(' ')[0];
        punch.formattedDate = punch.dateObj.toISOString().split('T')[0];

        if (!punch.user_name || punch.user_name === 'User ' + punch.user_id) {
            const found = this.state.users.find(u => u.user_id === punch.user_id || u.uid === punch.uid);
            if (found) punch.user_name = found.name;
        }

        this.state.lastPunch = punch;
        this.state.recentPunches.unshift(punch);
        if (this.state.recentPunches.length > 30) {
            this.state.recentPunches.pop();
        }

        // Add to device attendance list
        this.state.attendance.unshift({
            uid: punch.uid,
            user_id: punch.user_id,
            timestamp: punch.timestamp,
            status: punch.status,
            punch: punch.punch
        });

        // Refresh DB list & stats if currently viewing SQLite
        this.state.dbStats.total_records++;
        this.state.dbStats.today_records++;

        // Sound chime
        this.playPunchChime(punch.status);

        // Toast
        this.showToast('success', 'Absensi Masuk (SQLite Saved)', `${punch.user_name} (${punch.status_name}) pada ${punch.formattedTime}`);
        this.addLog(`Punch Masuk: ${punch.user_name} [${punch.user_id}] - ${punch.status_name} (Tersimpan ke SQLite)`, 'success');

        if (this.state.dataSource === 'sqlite') {
            this.loadDBAttendance();
        }

        this.render();
    },

    // ----------------------------------------------------
    // SQLite Database Sync & Query
    // ----------------------------------------------------
    async syncToDatabase() {
        if (!this.state.connected) {
            this.showToast('warning', 'Perangkat Belum Terhubung', 'Hubungkan ke mesin ZKTeco terlebih dahulu untuk sinkronisasi');
            return;
        }

        this.state.loading.syncing = true;
        this.render();

        try {
            const res = await fetch('/api/db/sync', { method: 'POST' });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Gagal sinkronisasi');

            const r = data.result || {};
            this.showToast('success', 'Sinkronisasi SQLite Selesai', `Total: ${r.total_device_records} | Baru Tersimpan: ${r.new_records} | Duplikat: ${r.skipped_duplicates}`);
            this.addLog(`Sinkronisasi SQLite: ${r.new_records} rekaman baru disimpan ke database`, 'success');

            await this.loadDBStats();
            if (this.state.dataSource === 'sqlite') {
                await this.loadDBAttendance();
            }
        } catch (e) {
            this.showToast('error', 'Gagal Sinkronisasi SQLite', e.message);
        } finally {
            this.state.loading.syncing = false;
            this.render();
        }
    },

    async loadDBStats() {
        try {
            const res = await fetch('/api/db/stats');
            if (res.ok) {
                this.state.dbStats = await res.json();
                this.render();
            }
        } catch (e) {}
    },

    async loadDBAttendance() {
        this.state.loading.attendance = true;
        this.render();

        try {
            const params = new URLSearchParams();
            if (this.state.attFilter.search) params.append('search', this.state.attFilter.search);
            if (this.state.attFilter.status && this.state.attFilter.status !== 'all') params.append('status', this.state.attFilter.status);
            if (this.state.attFilter.startDate) params.append('start_date', this.state.attFilter.startDate);
            if (this.state.attFilter.endDate) params.append('end_date', this.state.attFilter.endDate);
            params.append('page', this.state.attFilter.page);
            params.append('limit', this.state.attFilter.pageSize);

            const res = await fetch(`/api/db/attendance?${params.toString()}`);
            if (res.ok) {
                const data = await res.json();
                this.state.dbAttendance = data.records || [];
                this.state.dbTotalRecords = data.total || 0;

                // Sync recent punches if empty
                if (this.state.recentPunches.length === 0 && this.state.dbAttendance.length > 0) {
                    this.state.recentPunches = this.state.dbAttendance.slice(0, 10).map(l => ({
                        uid: l.uid,
                        user_id: l.user_id,
                        user_name: l.user_name || this.getUserName(l.user_id, l.uid),
                        timestamp: l.timestamp,
                        status: l.status,
                        status_name: l.status_name || this.getStatusName(l.status),
                        punch: l.punch,
                        formattedTime: new Date(l.timestamp).toTimeString().split(' ')[0],
                        formattedDate: new Date(l.timestamp).toISOString().split('T')[0]
                    }));
                    if (this.state.recentPunches.length > 0) {
                        this.state.lastPunch = this.state.recentPunches[0];
                    }
                }
            }
        } catch (e) {
            console.error('Error fetching SQLite attendance:', e);
        } finally {
            this.state.loading.attendance = false;
            this.render();
        }
    },

    // ----------------------------------------------------
    // API Calls
    // ----------------------------------------------------
    async checkStatus() {
        try {
            const res = await fetch('/api/status');
            const data = await res.json();
            this.state.connected = data.connected;
            if (data.config) this.state.config = data.config;
            this.state.uptimeSeconds = data.uptime_seconds || 0;

            if (this.state.connected) {
                await this.loadDeviceInfo();
                await this.loadUsers();
                await this.loadAttendance();
            }
        } catch (e) {
            console.error('Failed to get status:', e);
            this.state.connected = false;
        }
        this.render();
    },

    async refreshStatusOnly() {
        try {
            const res = await fetch('/api/status');
            const data = await res.json();
            this.state.connected = data.connected;
            this.render();
        } catch (e) {}
    },

    async loadDeviceInfo() {
        try {
            const res = await fetch('/api/info');
            if (res.ok) {
                this.state.info = await res.json();
            }
        } catch (e) {
            console.error('Failed to load device info:', e);
        }
    },

    async loadUsers() {
        if (!this.state.connected) return;
        this.state.loading.users = true;
        this.render();
        try {
            const res = await fetch('/api/users');
            if (res.ok) {
                this.state.users = await res.json() || [];
            }
        } catch (e) {
            this.showToast('error', 'Gagal memuat pengguna', e.message);
        } finally {
            this.state.loading.users = false;
            this.render();
        }
    },

    async loadAttendance() {
        if (!this.state.connected) return;
        this.state.loading.attendance = true;
        this.render();
        try {
            const res = await fetch('/api/attendance');
            if (res.ok) {
                const logs = await res.json() || [];
                logs.sort((a, b) => new Date(b.timestamp) - new Date(a.timestamp));
                this.state.attendance = logs;

                if (this.state.recentPunches.length === 0 && logs.length > 0) {
                    this.state.recentPunches = logs.slice(0, 10).map(l => ({
                        uid: l.uid,
                        user_id: l.user_id,
                        user_name: this.getUserName(l.user_id, l.uid),
                        timestamp: l.timestamp,
                        status: l.status,
                        status_name: this.getStatusName(l.status),
                        punch: l.punch,
                        formattedTime: new Date(l.timestamp).toTimeString().split(' ')[0],
                        formattedDate: new Date(l.timestamp).toISOString().split('T')[0]
                    }));
                    if (this.state.recentPunches.length > 0) {
                        this.state.lastPunch = this.state.recentPunches[0];
                    }
                }
            }
        } catch (e) {
            this.showToast('error', 'Gagal memuat log presensi dari mesin', e.message);
        } finally {
            this.state.loading.attendance = false;
            this.render();
        }
    },

    async loadTemplates() {
        if (!this.state.connected) return;
        this.state.loading.templates = true;
        this.render();
        try {
            const res = await fetch('/api/templates');
            if (res.ok) {
                this.state.templates = await res.json() || [];
            }
        } catch (e) {
            this.showToast('error', 'Gagal memuat template sidik jari', e.message);
        } finally {
            this.state.loading.templates = false;
            this.render();
        }
    },

    async connectDevice(config) {
        this.state.loading.connecting = true;
        this.render();
        try {
            const res = await fetch('/api/connect', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(config)
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Gagal terhubung');

            this.state.connected = true;
            this.state.config = config;
            this.state.modals.connect = false;
            this.showToast('success', 'Terhubung', `Berhasil terhubung ke mesin ${config.ip} ${config.mock ? '(Demo Mode)' : ''}`);
            this.addLog(`Terhubung ke ${config.ip}:${config.port} (Mock: ${config.mock})`, 'success');

            await this.loadDeviceInfo();
            await this.loadUsers();
            await this.loadAttendance();
            await this.syncToDatabase();
        } catch (e) {
            this.showToast('error', 'Koneksi Gagal', e.message);
            this.addLog(`Koneksi gagal ke ${config.ip}: ${e.message}`, 'error');
        } finally {
            this.state.loading.connecting = false;
            this.render();
        }
    },

    async disconnectDevice() {
        try {
            await fetch('/api/disconnect', { method: 'POST' });
            this.state.connected = false;
            this.state.info = null;
            this.showToast('info', 'Terputus', 'Koneksi ke mesin diakhiri.');
            this.addLog('Koneksi ke mesin diputus oleh pengguna', 'info');
        } catch (e) {
            this.showToast('error', 'Error', e.message);
        }
        this.render();
    },

    async saveUser(user) {
        this.state.loading.action = true;
        this.render();
        try {
            const res = await fetch('/api/users', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(user)
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Gagal menyimpan user');

            this.showToast('success', 'User Disimpan', `User ${user.name} (${user.user_id}) berhasil disimpan.`);
            this.addLog(`Simpan user: ${user.name} [${user.user_id}]`, 'success');
            this.state.modals.userForm = false;
            this.state.editingUser = null;
            await this.loadUsers();
        } catch (e) {
            this.showToast('error', 'Gagal Simpan User', e.message);
        } finally {
            this.state.loading.action = false;
            this.render();
        }
    },

    async deleteUser(uid, name) {
        this.state.loading.action = true;
        this.render();
        try {
            const res = await fetch(`/api/users/${uid}`, { method: 'DELETE' });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Gagal menghapus user');

            this.showToast('success', 'User Dihapus', `User ${name || uid} telah dihapus dari mesin.`);
            this.addLog(`Hapus user UID ${uid} (${name})`, 'warning');
            this.state.modals.deleteUser = false;
            this.state.selectedUserForDelete = null;
            await this.loadUsers();
        } catch (e) {
            this.showToast('error', 'Gagal Hapus User', e.message);
        } finally {
            this.state.loading.action = false;
            this.render();
        }
    },

    async clearAdmin() {
        this.state.loading.action = true;
        this.render();
        try {
            const res = await fetch('/api/admin/clear', { method: 'POST' });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Gagal mereset admin');

            this.showToast('success', 'Admin Direset', 'Seluruh hak akses admin telah dikembalikan menjadi user biasa.');
            this.addLog('Reset semua hak admin (Clear Admin)', 'warning');
            this.state.modals.clearAdmin = false;
            await this.loadUsers();
        } catch (e) {
            this.showToast('error', 'Gagal Clear Admin', e.message);
        } finally {
            this.state.loading.action = false;
            this.render();
        }
    },

    async deleteTemplate(uid, fid) {
        this.state.loading.action = true;
        this.render();
        try {
            const res = await fetch(`/api/templates/${uid}/${fid}`, { method: 'DELETE' });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Gagal menghapus template');

            this.showToast('success', 'Template Dihapus', `Template sidik jari (UID: ${uid}, Finger: ${fid}) telah dihapus.`);
            this.addLog(`Hapus sidik jari UID ${uid} FID ${fid}`, 'warning');
            this.state.modals.deleteTemplate = false;
            await this.loadTemplates();
        } catch (e) {
            this.showToast('error', 'Gagal Hapus Template', e.message);
        } finally {
            this.state.loading.action = false;
            this.render();
        }
    },

    async unlockDoor(seconds) {
        try {
            const res = await fetch('/api/device/unlock', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ seconds: parseInt(seconds) || 3 })
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Gagal membuka relay');

            this.showToast('success', 'Relay Aktif', `Kunci pintu dibuka selama ${seconds} detik.`);
            this.addLog(`Trigger relay kunci pintu: ${seconds} detik`, 'success');
        } catch (e) {
            this.showToast('error', 'Gagal Buka Pintu', e.message);
        }
    },

    async syncTime() {
        try {
            const res = await fetch('/api/device/synctime', { method: 'POST' });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Gagal sinkronisasi waktu');

            this.showToast('success', 'Jam Tersinkronisasi', `Waktu RTC mesin berhasil disamakan: ${data.timestamp}`);
            this.addLog(`Sinkronisasi waktu mesin ke ${data.timestamp}`, 'success');
        } catch (e) {
            this.showToast('error', 'Gagal Sinkronisasi Waktu', e.message);
        }
    },

    async playVoice(index) {
        try {
            const res = await fetch('/api/device/voice', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ index: parseInt(index) || 0 })
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Gagal memutar suara');

            this.showToast('info', 'Suara Diputar', `Suara prompt index #${index} dikirim ke speaker mesin.`);
            this.addLog(`Tes voice prompt index ${index}`, 'info');
        } catch (e) {
            this.showToast('error', 'Gagal Tes Suara', e.message);
        }
    },

    async writeLCD(line, text) {
        try {
            const res = await fetch('/api/device/lcd', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ line: parseInt(line) || 1, text: text || '' })
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Gagal menulis ke LCD');

            this.showToast('success', 'LCD Diperbarui', `Teks baris ${line} berhasil dikirim ke layar mesin.`);
            this.addLog(`Tulis teks LCD (Line ${line}): "${text}"`, 'info');
        } catch (e) {
            this.showToast('error', 'Gagal Kirim ke LCD', e.message);
        }
    },

    async clearLCD() {
        try {
            const res = await fetch('/api/device/lcd/clear', { method: 'POST' });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Gagal membersihkan LCD');

            this.showToast('info', 'LCD Dibersihkan', 'Teks layar LCD telah dibersihkan.');
            this.addLog('Layar LCD dibersihkan', 'info');
        } catch (e) {
            this.showToast('error', 'Gagal Bersihkan LCD', e.message);
        }
    },

    async restartDevice() {
        try {
            const res = await fetch('/api/device/restart', { method: 'POST' });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Gagal restart mesin');

            this.state.connected = false;
            this.state.modals.reboot = false;
            this.showToast('warning', 'Reboot Mesin', 'Perintah reboot telah dikirim. Mesin sedang memulai ulang...');
            this.addLog('Perintah Reboot dikirim ke mesin', 'warning');
            this.render();
        } catch (e) {
            this.showToast('error', 'Gagal Reboot', e.message);
        }
    },

    async powerOffDevice() {
        try {
            const res = await fetch('/api/device/poweroff', { method: 'POST' });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Gagal mematikan mesin');

            this.state.connected = false;
            this.state.modals.shutdown = false;
            this.showToast('warning', 'Shutdown Mesin', 'Mesin telah dimatikan.');
            this.addLog('Perintah Shutdown dikirim ke mesin', 'warning');
            this.render();
        } catch (e) {
            this.showToast('error', 'Gagal Shutdown', e.message);
        }
    },

    async triggerDemoPunch(userId, status) {
        try {
            const res = await fetch('/api/demo/punch', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ user_id: userId || '1001', status: parseInt(status) || 0 })
            });
            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'Gagal trigger simulasi');
        } catch (e) {
            this.showToast('error', 'Simulasi Gagal', e.message);
        }
    },

    // ----------------------------------------------------
    // Utility & Formatting Helpers
    // ----------------------------------------------------
    getUserName(userId, uid) {
        const found = this.state.users.find(u => u.user_id === String(userId) || u.uid === Number(uid));
        return found ? found.name : `Pegawai ${userId}`;
    },

    getStatusName(status) {
        switch (Number(status)) {
            case 0: return 'Check-In';
            case 1: return 'Check-Out';
            case 2: return 'Break-Out';
            case 3: return 'Break-In';
            case 4: return 'OT-In';
            case 5: return 'OT-Out';
            default: return `Status(${status})`;
        }
    },

    getFingerName(fid) {
        const names = [
            'Ibu Jari Kiri', 'Telunjuk Kiri', 'Jari Tengah Kiri', 'Jari Manis Kiri', 'Kelingking Kiri',
            'Ibu Jari Kanan', 'Telunjuk Kanan', 'Jari Tengah Kanan', 'Jari Manis Kanan', 'Kelingking Kanan'
        ];
        return names[fid] || `Jari #${fid}`;
    },

    getRoleName(privilege) {
        const p = Number(privilege);
        const isDisabled = (p & 1) !== 0;
        const roleMask = p & 0x0E;
        let role = 'User';
        if (roleMask === 14) role = 'Admin';
        else if (roleMask === 4) role = 'Manager';
        else if (roleMask === 2) role = 'Enroller';

        return { role, isDisabled };
    },

    formatDuration(secs) {
        if (!secs || secs < 0) return '0d';
        const h = Math.floor(secs / 3600);
        const m = Math.floor((secs % 3600) / 60);
        const s = secs % 60;
        if (h > 0) return `${h}j ${m}m ${s}d`;
        if (m > 0) return `${m}m ${s}d`;
        return `${s} detik`;
    },

    addLog(message, type = 'info') {
        const time = new Date().toTimeString().split(' ')[0];
        this.state.logs.unshift({ time, message, type });
        if (this.state.logs.length > 50) this.state.logs.pop();
        const logContainer = document.getElementById('log-console');
        if (logContainer) {
            this.renderLogs();
        }
    },

    showToast(type, title, message) {
        const toastContainer = document.getElementById('toast-container');
        if (!toastContainer) return;

        const toast = document.createElement('div');
        const bgColors = {
            success: 'bg-emerald-600 text-white shadow-emerald-500/30',
            error: 'bg-rose-600 text-white shadow-rose-500/30',
            warning: 'bg-amber-600 text-white shadow-amber-500/30',
            info: 'bg-indigo-600 text-white shadow-indigo-500/30'
        };

        const icons = {
            success: '✓',
            error: '✕',
            warning: '⚠',
            info: 'ℹ'
        };

        toast.className = `flex items-center gap-3 p-4 rounded-xl shadow-lg transform transition-all duration-300 translate-y-2 opacity-0 ${bgColors[type] || bgColors.info}`;
        toast.innerHTML = `
            <div class="flex-shrink-0 w-7 h-7 rounded-full bg-white/20 flex items-center justify-center font-bold text-sm">
                ${icons[type] || 'ℹ'}
            </div>
            <div class="flex-1 pr-2">
                <div class="font-semibold text-sm leading-tight">${title}</div>
                <div class="text-xs opacity-90 leading-tight mt-0.5">${message}</div>
            </div>
            <button class="opacity-70 hover:opacity-100 p-1 text-xs">✕</button>
        `;

        toast.querySelector('button').onclick = () => {
            toast.classList.add('opacity-0', 'translate-y-2');
            setTimeout(() => toast.remove(), 300);
        };

        toastContainer.appendChild(toast);
        requestAnimationFrame(() => {
            toast.classList.remove('translate-y-2', 'opacity-0');
        });

        setTimeout(() => {
            if (toast.parentElement) {
                toast.classList.add('opacity-0', 'translate-y-2');
                setTimeout(() => toast.remove(), 300);
            }
        }, 4000);
    },

    exportAttendanceJSON() {
        const data = this.state.dataSource === 'sqlite' ? this.state.dbAttendance : this.state.attendance;
        const dataStr = "data:text/json;charset=utf-8," + encodeURIComponent(JSON.stringify(data, null, 2));
        const a = document.createElement('a');
        a.setAttribute("href", dataStr);
        a.setAttribute("download", `attendance_export_${new Date().toISOString().slice(0,10)}.json`);
        document.body.appendChild(a);
        a.click();
        a.remove();
    },

    exportUsersJSON() {
        const dataStr = "data:text/json;charset=utf-8," + encodeURIComponent(JSON.stringify(this.state.users, null, 2));
        const a = document.createElement('a');
        a.setAttribute("href", dataStr);
        a.setAttribute("download", `users_export_${new Date().toISOString().slice(0,10)}.json`);
        document.body.appendChild(a);
        a.click();
        a.remove();
    },

    // ----------------------------------------------------
    // Rendering Engine
    // ----------------------------------------------------
    render() {
        this.renderHeaderStatus();
        this.renderTabs();
        this.renderLogs();

        switch (this.state.activeTab) {
            case 'dashboard':
                this.renderDashboard();
                break;
            case 'live':
                this.renderLiveMonitor();
                break;
            case 'users':
                this.renderUsers();
                break;
            case 'attendance':
                this.renderAttendance();
                break;
            case 'templates':
                this.renderTemplates();
                break;
            case 'diagnostics':
                this.renderDiagnostics();
                break;
        }

        this.renderModals();
    },

    renderHeaderStatus() {
        const statusBadge = document.getElementById('header-status-badge');
        const connBtn = document.getElementById('btn-header-connect');
        const audioBtn = document.getElementById('btn-header-audio');
        const themeBtn = document.getElementById('btn-header-theme');
        const dbBadge = document.getElementById('header-db-badge');

        if (statusBadge) {
            if (this.state.connected) {
                const isMock = this.state.config.mock;
                statusBadge.className = `flex items-center gap-2 px-3 py-1.5 rounded-full text-xs font-semibold ${isMock ? 'bg-amber-500/10 text-amber-400 border border-amber-500/30' : 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/30'}`;
                statusBadge.innerHTML = `
                    <span class="relative flex h-2 w-2">
                        <span class="animate-ping absolute inline-flex h-full w-full rounded-full ${isMock ? 'bg-amber-400' : 'bg-emerald-400'} opacity-75"></span>
                        <span class="relative inline-flex rounded-full h-2 w-2 ${isMock ? 'bg-amber-500' : 'bg-emerald-500'}"></span>
                    </span>
                    <span>${isMock ? 'DEMO MODE' : `${this.state.config.ip}:${this.state.config.port}`}</span>
                `;
            } else {
                statusBadge.className = "flex items-center gap-2 px-3 py-1.5 rounded-full text-xs font-semibold bg-rose-500/10 text-rose-400 border border-rose-500/30";
                statusBadge.innerHTML = `
                    <span class="inline-flex rounded-full h-2 w-2 bg-rose-500"></span>
                    <span>TERPUTUS</span>
                `;
            }
        }

        if (dbBadge) {
            dbBadge.textContent = `${this.state.dbStats.total_records.toLocaleString()} Rekaman SQLite`;
        }

        if (connBtn) {
            if (this.state.connected) {
                connBtn.className = "px-3 py-1.5 text-xs font-medium rounded-lg bg-rose-600/20 hover:bg-rose-600/30 text-rose-300 border border-rose-500/30 transition-all flex items-center gap-1.5";
                connBtn.innerHTML = `
                    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/></svg>
                    <span>Putus Koneksi</span>
                `;
                connBtn.onclick = () => this.disconnectDevice();
            } else {
                connBtn.className = "px-3 py-1.5 text-xs font-medium rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white shadow-lg shadow-emerald-600/20 transition-all flex items-center gap-1.5";
                connBtn.innerHTML = `
                    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z"/></svg>
                    <span>Hubungkan Mesin</span>
                `;
                connBtn.onclick = () => { this.state.modals.connect = true; this.render(); };
            }
        }

        if (audioBtn) {
            audioBtn.innerHTML = this.state.audioEnabled
                ? `<svg class="w-4 h-4 text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.536 8.464a5 5 0 010 7.072m2.828-9.9a9 9 0 010 12.728M5.586 15H4a1 1 0 01-1-1v-4a1 1 0 011-1h1.586l4.707-4.707C10.923 3.663 12 4.109 12 5v14c0 .891-1.077 1.337-1.707.707L5.586 15z"/></svg>`
                : `<svg class="w-4 h-4 text-slate-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5.586 15H4a1 1 0 01-1-1v-4a1 1 0 011-1h1.586l4.707-4.707C10.923 3.663 12 4.109 12 5v14c0 .891-1.077 1.337-1.707.707L5.586 15zM17 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2"/></svg>`;
        }

        if (themeBtn) {
            themeBtn.innerHTML = this.state.darkMode
                ? `<svg class="w-4 h-4 text-amber-300" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z"/></svg>`
                : `<svg class="w-4 h-4 text-slate-700" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z"/></svg>`;
        }
    },

    renderTabs() {
        document.querySelectorAll('.nav-tab-btn').forEach(btn => {
            const tabName = btn.dataset.tab;
            if (tabName === this.state.activeTab) {
                btn.className = "nav-tab-btn flex items-center gap-3 px-3.5 py-2.5 rounded-xl text-sm font-semibold bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border border-emerald-500/30 transition-all shadow-sm";
            } else {
                btn.className = "nav-tab-btn flex items-center gap-3 px-3.5 py-2.5 rounded-xl text-sm font-medium text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100 hover:bg-slate-100 dark:hover:bg-slate-800/60 transition-all";
            }
        });

        document.querySelectorAll('.tab-view').forEach(view => {
            if (view.id === `view-${this.state.activeTab}`) {
                view.classList.remove('hidden');
            } else {
                view.classList.add('hidden');
            }
        });
    },

    renderDashboard() {
        const info = this.state.info || {};
        const sizes = info.sizes || {};

        const elDeviceName = document.getElementById('dash-device-name');
        const elSerial = document.getElementById('dash-serial');
        const elFirmware = document.getElementById('dash-firmware');
        const elPlatform = document.getElementById('dash-platform');
        const elMAC = document.getElementById('dash-mac');
        const elNetwork = document.getElementById('dash-network');
        const elUptime = document.getElementById('dash-uptime');

        if (elDeviceName) elDeviceName.textContent = info.device_name || 'ZKTeco Standalone Device';
        if (elSerial) elSerial.textContent = info.serial_number || '-';
        if (elFirmware) elFirmware.textContent = info.firmware_version || '-';
        if (elPlatform) elPlatform.textContent = info.platform || '-';
        if (elMAC) elMAC.textContent = info.mac || '-';
        if (elNetwork && info.network) {
            elNetwork.textContent = `${info.network.ip || this.state.config.ip} / ${info.network.mask || '255.255.255.0'}`;
        }
        if (elUptime) elUptime.textContent = this.formatDuration(this.state.uptimeSeconds);

        this.updateGauge('dash-users-gauge', 'dash-users-text', sizes.users || this.state.users.length, sizes.users_capacity || 1000);
        this.updateGauge('dash-fingers-gauge', 'dash-fingers-text', sizes.fingers || this.state.templates.length, sizes.fingers_capacity || 2000);
        this.updateGauge('dash-records-gauge', 'dash-records-text', sizes.records || this.state.attendance.length, sizes.records_capacity || 50000);
        this.updateGauge('dash-faces-gauge', 'dash-faces-text', sizes.faces || 0, sizes.faces_capacity || 500);

        const miniFeed = document.getElementById('dash-recent-punches');
        if (miniFeed) {
            if (this.state.recentPunches.length === 0) {
                miniFeed.innerHTML = `<div class="p-6 text-center text-slate-400 text-sm">Belum ada aktivitas presensi terbaru</div>`;
            } else {
                miniFeed.innerHTML = this.state.recentPunches.slice(0, 6).map(p => {
                    const statusColor = p.status === 0 ? 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30' : 'bg-amber-500/15 text-amber-400 border-amber-500/30';
                    return `
                        <div class="flex items-center justify-between p-3 rounded-xl bg-slate-800/40 border border-slate-800/80 hover:bg-slate-800/70 transition-all">
                            <div class="flex items-center gap-3">
                                <div class="w-10 h-10 rounded-xl bg-gradient-to-br from-emerald-500/20 to-teal-500/20 border border-emerald-500/30 flex items-center justify-center font-bold text-emerald-400 text-sm">
                                    ${p.user_name ? p.user_name.substring(0,2).toUpperCase() : 'US'}
                                </div>
                                <div>
                                    <div class="font-semibold text-sm text-slate-200">${p.user_name}</div>
                                    <div class="text-xs text-slate-400">ID: ${p.user_id} • UID: ${p.uid}</div>
                                </div>
                            </div>
                            <div class="text-right">
                                <span class="px-2.5 py-0.5 rounded-full text-xs font-semibold border ${statusColor}">${p.status_name}</span>
                                <div class="text-xs text-slate-400 font-mono mt-1">${p.formattedTime || new Date(p.timestamp).toTimeString().split(' ')[0]}</div>
                            </div>
                        </div>
                    `;
                }).join('');
            }
        }
    },

    updateGauge(barId, textId, current, total) {
        const bar = document.getElementById(barId);
        const text = document.getElementById(textId);
        const curr = current || 0;
        const tot = total || 1;
        const pct = Math.min(100, Math.round((curr / tot) * 100));

        if (bar) bar.style.width = `${pct}%`;
        if (text) text.textContent = `${curr.toLocaleString()} / ${tot.toLocaleString()} (${pct}%)`;
    },

    renderLiveMonitor() {
        const last = this.state.lastPunch;
        const heroEl = document.getElementById('kiosk-hero-card');

        if (heroEl) {
            if (last) {
                const isCheckIn = last.status === 0;
                heroEl.className = `p-6 rounded-2xl border transition-all duration-500 flex flex-col md:flex-row items-center gap-6 ${isCheckIn ? 'bg-emerald-950/40 border-emerald-500/40 glow-emerald' : 'bg-amber-950/40 border-amber-500/40'}`;
                heroEl.innerHTML = `
                    <div class="w-24 h-24 rounded-2xl bg-gradient-to-br from-emerald-500 to-teal-600 flex items-center justify-center text-white text-3xl font-black shadow-xl shadow-emerald-500/30">
                        ${last.user_name ? last.user_name.substring(0,2).toUpperCase() : 'US'}
                    </div>
                    <div class="flex-1 text-center md:text-left">
                        <div class="inline-flex items-center gap-2 px-3 py-1 rounded-full text-xs font-bold uppercase tracking-wider mb-2 ${isCheckIn ? 'bg-emerald-500 text-emerald-950' : 'bg-amber-500 text-amber-950'}">
                            ${last.status_name} BERHASIL (SQLITE SAVED)
                        </div>
                        <h2 class="text-3xl font-extrabold text-white tracking-tight">${last.user_name}</h2>
                        <div class="text-slate-300 font-mono mt-1 flex flex-wrap gap-4 justify-center md:justify-start text-sm">
                            <span>User ID: <strong class="text-white">${last.user_id}</strong></span>
                            <span>UID: <strong class="text-white">${last.uid}</strong></span>
                            <span>Waktu: <strong class="text-emerald-400">${last.formattedTime || new Date(last.timestamp).toTimeString().split(' ')[0]}</strong></span>
                        </div>
                    </div>
                    <div class="text-center bg-slate-900/60 p-4 rounded-xl border border-slate-800">
                        <div class="text-xs text-slate-400 uppercase tracking-widest font-semibold">Tipe Absensi</div>
                        <div class="text-xl font-black mt-1 ${isCheckIn ? 'text-emerald-400' : 'text-amber-400'}">${last.status_name}</div>
                    </div>
                `;
            } else {
                heroEl.className = "p-8 rounded-2xl border border-slate-800 bg-slate-900/50 text-center";
                heroEl.innerHTML = `
                    <div class="max-w-md mx-auto">
                        <div class="w-16 h-16 rounded-2xl bg-slate-800 flex items-center justify-center mx-auto text-slate-400 mb-3">
                            <svg class="w-8 h-8 animate-pulse text-emerald-400" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 11c0 3.517-1.009 6.799-2.753 9.571m-3.44-2.04l.054-.09A13.916 13.916 0 008 11a4 4 0 118 0c0 1.017-.07 2.019-.203 3m-2.118 6.844A21.88 21.88 0 0015.171 17m3.839 1.132c.645-2.266.99-4.659.99-7.132A8 8 0 004.07 3.755"/></svg>
                        </div>
                        <h3 class="text-xl font-bold text-white">Menunggu Scan Biometrik...</h3>
                        <p class="text-sm text-slate-400 mt-1">Setiap punch langsung dicatat secara otomatis ke database SQLite lokal.</p>
                    </div>
                `;
            }
        }

        const liveTable = document.getElementById('kiosk-stream-table');
        if (liveTable) {
            if (this.state.recentPunches.length === 0) {
                liveTable.innerHTML = `<tr><td colspan="5" class="py-8 text-center text-slate-500">Belum ada data presensi live</td></tr>`;
            } else {
                liveTable.innerHTML = this.state.recentPunches.map((p, idx) => {
                    const isCheckIn = p.status === 0;
                    const badgeClass = isCheckIn ? 'bg-emerald-500/20 text-emerald-300 border-emerald-500/30' : 'bg-amber-500/20 text-amber-300 border-amber-500/30';
                    return `
                        <tr class="border-b border-slate-800/60 hover:bg-slate-800/30 transition-colors ${idx === 0 ? 'bg-emerald-500/5' : ''}">
                            <td class="py-3 px-4 font-mono text-sm text-emerald-400 font-bold">${p.formattedTime || new Date(p.timestamp).toTimeString().split(' ')[0]}</td>
                            <td class="py-3 px-4 font-mono text-sm text-slate-300">${p.user_id}</td>
                            <td class="py-3 px-4 font-semibold text-slate-100">${p.user_name}</td>
                            <td class="py-3 px-4">
                                <span class="px-2.5 py-1 rounded-full text-xs font-bold border ${badgeClass}">${p.status_name}</span>
                            </td>
                            <td class="py-3 px-4 text-xs font-mono text-slate-400">UID: ${p.uid} (Punch ${p.punch})</td>
                        </tr>
                    `;
                }).join('');
            }
        }
    },

    renderUsers() {
        const listEl = document.getElementById('users-table-body');
        const countEl = document.getElementById('users-count-badge');
        if (!listEl) return;

        let filtered = this.state.users;

        const q = this.state.userFilter.search.toLowerCase().trim();
        if (q) {
            filtered = filtered.filter(u =>
                (u.name && u.name.toLowerCase().includes(q)) ||
                (u.user_id && u.user_id.toLowerCase().includes(q)) ||
                (String(u.uid).includes(q)) ||
                (String(u.card).includes(q))
            );
        }

        const role = this.state.userFilter.role;
        if (role !== 'all') {
            filtered = filtered.filter(u => {
                const info = this.getRoleName(u.privilege);
                if (role === 'admin') return info.role === 'Admin';
                if (role === 'manager') return info.role === 'Manager';
                if (role === 'enroller') return info.role === 'Enroller';
                if (role === 'user') return info.role === 'User' && !info.isDisabled;
                if (role === 'disabled') return info.isDisabled;
                return true;
            });
        }

        if (countEl) countEl.textContent = `${filtered.length} Pengguna`;

        if (filtered.length === 0) {
            listEl.innerHTML = `<tr><td colspan="7" class="py-12 text-center text-slate-400">Tidak ada pengguna yang cocok dengan kriteria pencarian</td></tr>`;
            return;
        }

        listEl.innerHTML = filtered.map(u => {
            const roleInfo = this.getRoleName(u.privilege);
            let roleBadge = 'bg-slate-700/50 text-slate-300 border-slate-600';
            if (roleInfo.role === 'Admin') roleBadge = 'bg-rose-500/20 text-rose-300 border-rose-500/40';
            else if (roleInfo.role === 'Manager') roleBadge = 'bg-amber-500/20 text-amber-300 border-amber-500/40';
            else if (roleInfo.role === 'Enroller') roleBadge = 'bg-cyan-500/20 text-cyan-300 border-cyan-500/40';

            return `
                <tr class="border-b border-slate-800/60 hover:bg-slate-800/40 transition-colors">
                    <td class="py-3.5 px-4 font-mono text-xs text-slate-400">${u.uid}</td>
                    <td class="py-3.5 px-4 font-mono font-bold text-sm text-emerald-400">${u.user_id}</td>
                    <td class="py-3.5 px-4">
                        <div class="font-semibold text-slate-100">${u.name}</div>
                        ${u.card > 0 ? `<div class="text-xs text-slate-400 font-mono">Kartu: ${u.card}</div>` : ''}
                    </td>
                    <td class="py-3.5 px-4">
                        <div class="flex items-center gap-1.5">
                            <span class="px-2.5 py-0.5 rounded-full text-xs font-semibold border ${roleBadge}">${roleInfo.role}</span>
                            ${roleInfo.isDisabled ? `<span class="px-2 py-0.5 rounded-full text-xs bg-rose-950 text-rose-400 border border-rose-800 font-bold">NONAKTIF</span>` : ''}
                        </div>
                    </td>
                    <td class="py-3.5 px-4 font-mono text-xs text-slate-400">${u.password ? '••••••' : '-'}</td>
                    <td class="py-3.5 px-4 font-mono text-xs text-slate-400">${u.group_id || '1'}</td>
                    <td class="py-3.5 px-4 text-right">
                        <div class="flex items-center justify-end gap-1.5">
                            <button onclick="App.openEditUserModal(${u.uid})" class="p-1.5 rounded-lg bg-slate-800 hover:bg-slate-700 text-slate-300 hover:text-white transition-colors" title="Edit Profil">
                                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z"/></svg>
                            </button>
                            <button onclick="App.confirmDeleteUser(${u.uid}, '${u.name.replace(/'/g, "\\'")}')" class="p-1.5 rounded-lg bg-rose-950/40 hover:bg-rose-900/60 text-rose-400 hover:text-rose-200 border border-rose-800/40 transition-colors" title="Hapus User">
                                <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
                            </button>
                        </div>
                    </td>
                </tr>
            `;
        }).join('');
    },

    renderAttendance() {
        const isSQLite = this.state.dataSource === 'sqlite';
        const listEl = document.getElementById('att-table-body');
        const countEl = document.getElementById('att-total-badge');
        const todayCountEl = document.getElementById('att-today-badge');
        const uniqueCountEl = document.getElementById('att-unique-badge');
        const sourceBtnSQLite = document.getElementById('btn-source-sqlite');
        const sourceBtnDevice = document.getElementById('btn-source-device');

        if (sourceBtnSQLite && sourceBtnDevice) {
            if (isSQLite) {
                sourceBtnSQLite.className = "px-3 py-1.5 rounded-lg text-xs font-bold bg-emerald-600 text-white shadow-sm";
                sourceBtnDevice.className = "px-3 py-1.5 rounded-lg text-xs font-medium text-slate-400 hover:text-slate-200";
            } else {
                sourceBtnSQLite.className = "px-3 py-1.5 rounded-lg text-xs font-medium text-slate-400 hover:text-slate-200";
                sourceBtnDevice.className = "px-3 py-1.5 rounded-lg text-xs font-bold bg-emerald-600 text-white shadow-sm";
            }
        }

        if (!listEl) return;

        if (isSQLite) {
            // Render from SQLite Data
            const stats = this.state.dbStats;
            if (countEl) countEl.textContent = `${this.state.dbTotalRecords.toLocaleString()} Rekaman SQLite`;
            if (todayCountEl) todayCountEl.textContent = (stats.today_records || 0).toLocaleString();
            if (uniqueCountEl) uniqueCountEl.textContent = (stats.today_unique_users || 0).toLocaleString();

            const totalPages = Math.ceil(this.state.dbTotalRecords / this.state.attFilter.pageSize) || 1;
            this.renderPaginationControls(this.state.attFilter.page, totalPages, this.state.dbTotalRecords);

            if (this.state.dbAttendance.length === 0) {
                listEl.innerHTML = `<tr><td colspan="7" class="py-12 text-center text-slate-400">Tidak ada log di database SQLite yang sesuai dengan filter</td></tr>`;
                return;
            }

            listEl.innerHTML = this.state.dbAttendance.map(r => {
                const dateObj = new Date(r.timestamp);
                const timeStr = isNaN(dateObj) ? r.timestamp : dateObj.toLocaleString('id-ID', { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' });
                const statusName = r.status_name || this.getStatusName(r.status);
                const isCheckIn = r.status === 0;

                return `
                    <tr class="border-b border-slate-800/60 hover:bg-slate-800/40 transition-colors">
                        <td class="py-3.5 px-4 font-mono text-xs text-slate-500">#${r.id}</td>
                        <td class="py-3.5 px-4 font-mono font-bold text-sm text-emerald-400">${r.user_id}</td>
                        <td class="py-3.5 px-4 font-semibold text-slate-200">${r.user_name || this.getUserName(r.user_id, r.uid)}</td>
                        <td class="py-3.5 px-4 font-mono text-xs text-slate-300">${timeStr}</td>
                        <td class="py-3.5 px-4">
                            <span class="px-2.5 py-0.5 rounded-full text-xs font-semibold border ${isCheckIn ? 'bg-emerald-500/20 text-emerald-300 border-emerald-500/40' : 'bg-amber-500/20 text-amber-300 border-amber-500/40'}">
                                ${statusName}
                            </span>
                        </td>
                        <td class="py-3.5 px-4 font-mono text-xs text-slate-400">${r.source || 'device'}</td>
                        <td class="py-3.5 px-4 font-mono text-xs text-slate-500">${r.punch}</td>
                    </tr>
                `;
            }).join('');

        } else {
            // Render from Live Device RAM
            let records = this.state.attendance;
            const now = new Date();
            const todayStr = now.toISOString().split('T')[0];
            const todayLogs = records.filter(r => r.timestamp && r.timestamp.startsWith(todayStr));
            const uniqueTodayUsers = new Set(todayLogs.map(r => r.user_id)).size;

            if (countEl) countEl.textContent = `${records.length.toLocaleString()} Rekaman Mesin`;
            if (todayCountEl) todayCountEl.textContent = todayLogs.length.toLocaleString();
            if (uniqueCountEl) uniqueCountEl.textContent = uniqueTodayUsers.toLocaleString();

            const q = this.state.attFilter.search.toLowerCase().trim();
            if (q) {
                records = records.filter(r => {
                    const name = this.getUserName(r.user_id, r.uid).toLowerCase();
                    return String(r.user_id).includes(q) || String(r.uid).includes(q) || name.includes(q);
                });
            }

            const status = this.state.attFilter.status;
            if (status !== 'all') {
                records = records.filter(r => String(r.status) === status);
            }

            if (this.state.attFilter.startDate) {
                records = records.filter(r => r.timestamp && r.timestamp.slice(0, 10) >= this.state.attFilter.startDate);
            }
            if (this.state.attFilter.endDate) {
                records = records.filter(r => r.timestamp && r.timestamp.slice(0, 10) <= this.state.attFilter.endDate);
            }

            const totalItems = records.length;
            const pageSize = this.state.attFilter.pageSize;
            const totalPages = Math.ceil(totalItems / pageSize) || 1;
            const page = Math.min(this.state.attFilter.page, totalPages);
            const startIndex = (page - 1) * pageSize;
            const paginated = records.slice(startIndex, startIndex + pageSize);

            this.renderPaginationControls(page, totalPages, totalItems);

            if (paginated.length === 0) {
                listEl.innerHTML = `<tr><td colspan="7" class="py-12 text-center text-slate-400">Tidak ada data log yang sesuai dengan filter</td></tr>`;
                return;
            }

            listEl.innerHTML = paginated.map(r => {
                const dateObj = new Date(r.timestamp);
                const timeStr = isNaN(dateObj) ? r.timestamp : dateObj.toLocaleString('id-ID', { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' });
                const userName = this.getUserName(r.user_id, r.uid);
                const statusName = this.getStatusName(r.status);
                const isCheckIn = r.status === 0;

                return `
                    <tr class="border-b border-slate-800/60 hover:bg-slate-800/40 transition-colors">
                        <td class="py-3.5 px-4 font-mono text-xs text-slate-400">${r.uid}</td>
                        <td class="py-3.5 px-4 font-mono font-bold text-sm text-emerald-400">${r.user_id}</td>
                        <td class="py-3.5 px-4 font-semibold text-slate-200">${userName}</td>
                        <td class="py-3.5 px-4 font-mono text-xs text-slate-300">${timeStr}</td>
                        <td class="py-3.5 px-4">
                            <span class="px-2.5 py-0.5 rounded-full text-xs font-semibold border ${isCheckIn ? 'bg-emerald-500/20 text-emerald-300 border-emerald-500/40' : 'bg-amber-500/20 text-amber-300 border-amber-500/40'}">
                                ${statusName}
                            </span>
                        </td>
                        <td class="py-3.5 px-4 font-mono text-xs text-slate-400">RAM Mesin</td>
                        <td class="py-3.5 px-4 font-mono text-xs text-slate-500">${r.punch}</td>
                    </tr>
                `;
            }).join('');
        }
    },

    renderPaginationControls(page, totalPages, totalItems) {
        const pageEl = document.getElementById('att-pagination-info');
        const prevBtn = document.getElementById('att-prev-page');
        const nextBtn = document.getElementById('att-next-page');

        if (pageEl) {
            pageEl.textContent = `Halaman ${page} dari ${totalPages} (${totalItems} total)`;
        }
        if (prevBtn) {
            prevBtn.disabled = page <= 1;
            prevBtn.onclick = () => {
                this.state.attFilter.page = Math.max(1, page - 1);
                if (this.state.dataSource === 'sqlite') {
                    this.loadDBAttendance();
                } else {
                    this.render();
                }
            };
        }
        if (nextBtn) {
            nextBtn.disabled = page >= totalPages;
            nextBtn.onclick = () => {
                this.state.attFilter.page = Math.min(totalPages, page + 1);
                if (this.state.dataSource === 'sqlite') {
                    this.loadDBAttendance();
                } else {
                    this.render();
                }
            };
        }
    },

    renderTemplates() {
        const listEl = document.getElementById('templates-table-body');
        const countEl = document.getElementById('templates-count-badge');
        if (!listEl) return;

        if (countEl) countEl.textContent = `${this.state.templates.length} Template`;

        if (this.state.templates.length === 0) {
            listEl.innerHTML = `<tr><td colspan="6" class="py-12 text-center text-slate-400">Tidak ada data template biometrik sidik jari yang terdaftar di mesin</td></tr>`;
            return;
        }

        listEl.innerHTML = this.state.templates.map(t => {
            const userName = this.getUserName(t.uid, t.uid);
            const fingerLabel = this.getFingerName(t.fid);

            return `
                <tr class="border-b border-slate-800/60 hover:bg-slate-800/40 transition-colors">
                    <td class="py-3.5 px-4 font-mono text-xs text-slate-400">${t.uid}</td>
                    <td class="py-3.5 px-4 font-semibold text-slate-200">${userName}</td>
                    <td class="py-3.5 px-4">
                        <div class="flex items-center gap-2">
                            <span class="w-6 h-6 rounded-lg bg-teal-500/20 text-teal-400 border border-teal-500/30 flex items-center justify-center text-xs font-bold font-mono">${t.fid}</span>
                            <span class="text-sm font-medium text-slate-200">${fingerLabel}</span>
                        </div>
                    </td>
                    <td class="py-3.5 px-4 font-mono text-xs text-emerald-400 tracking-wider">${t.mark || '-'}</td>
                    <td class="py-3.5 px-4 font-mono text-xs text-slate-400">${t.size} B</td>
                    <td class="py-3.5 px-4 text-right">
                        <button onclick="App.confirmDeleteTemplate(${t.uid}, ${t.fid})" class="p-1.5 rounded-lg bg-rose-950/40 hover:bg-rose-900/60 text-rose-400 hover:text-rose-200 border border-rose-800/40 transition-colors" title="Hapus Template">
                            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
                        </button>
                    </td>
                </tr>
            `;
        }).join('');
    },

    renderDiagnostics() {
        const lcdPreview = document.getElementById('virtual-lcd-preview');
        if (lcdPreview) {
            lcdPreview.innerHTML = `
                <div class="text-sm tracking-widest font-mono select-none leading-relaxed">
                    <div>1: ${this.escapeHTML(this.state.lcdInput.text || 'ZKTeco Standalone')}</div>
                    <div>2: IP ${this.state.config.ip || '192.168.1.201'}</div>
                </div>
            `;
        }
    },

    renderLogs() {
        const logContainer = document.getElementById('log-console');
        if (!logContainer) return;

        logContainer.innerHTML = this.state.logs.map(l => {
            let color = 'text-slate-300';
            if (l.type === 'success') color = 'text-emerald-400';
            else if (l.type === 'error') color = 'text-rose-400';
            else if (l.type === 'warning') color = 'text-amber-400';

            return `<div class="font-mono text-xs leading-relaxed"><span class="text-slate-500">[${l.time}]</span> <span class="${color}">${this.escapeHTML(l.message)}</span></div>`;
        }).join('');
    },

    renderModals() {
        Object.keys(this.state.modals).forEach(modalName => {
            const modalEl = document.getElementById(`modal-${modalName}`);
            if (modalEl) {
                if (this.state.modals[modalName]) {
                    modalEl.classList.remove('hidden');
                } else {
                    modalEl.classList.add('hidden');
                }
            }
        });
    },

    // ----------------------------------------------------
    // Modal Open/Close Actions
    // ----------------------------------------------------
    openAddUserModal() {
        this.state.editingUser = {
            uid: 0,
            user_id: '',
            name: '',
            privilege: 0,
            password: '',
            group_id: '1',
            card: 0
        };
        const titleEl = document.getElementById('modal-userForm-title');
        if (titleEl) titleEl.textContent = 'Tambah Pengguna Baru';
        this.populateUserForm(this.state.editingUser);
        this.state.modals.userForm = true;
        this.render();
    },

    openEditUserModal(uid) {
        const found = this.state.users.find(u => u.uid === uid);
        if (!found) return;

        this.state.editingUser = { ...found };
        const titleEl = document.getElementById('modal-userForm-title');
        if (titleEl) titleEl.textContent = `Edit Pengguna #${found.user_id}`;
        this.populateUserForm(this.state.editingUser);
        this.state.modals.userForm = true;
        this.render();
    },

    populateUserForm(u) {
        const inpUid = document.getElementById('form-user-uid');
        const inpUserId = document.getElementById('form-user-id');
        const inpName = document.getElementById('form-user-name');
        const selRole = document.getElementById('form-user-role');
        const inpPwd = document.getElementById('form-user-pwd');
        const inpCard = document.getElementById('form-user-card');
        const inpGroup = document.getElementById('form-user-group');

        if (inpUid) inpUid.value = u.uid || 0;
        if (inpUserId) inpUserId.value = u.user_id || '';
        if (inpName) inpName.value = u.name || '';
        if (selRole) selRole.value = u.privilege || 0;
        if (inpPwd) inpPwd.value = u.password || '';
        if (inpCard) inpCard.value = u.card || 0;
        if (inpGroup) inpGroup.value = u.group_id || '1';
    },

    submitUserForm() {
        const user = {
            uid: parseInt(document.getElementById('form-user-uid').value) || 0,
            user_id: document.getElementById('form-user-id').value.trim(),
            name: document.getElementById('form-user-name').value.trim(),
            privilege: parseInt(document.getElementById('form-user-role').value) || 0,
            password: document.getElementById('form-user-pwd').value.trim(),
            card: parseInt(document.getElementById('form-user-card').value) || 0,
            group_id: document.getElementById('form-user-group').value.trim() || '1'
        };

        if (!user.name) {
            this.showToast('error', 'Form Belum Lengkap', 'Nama pengguna wajib diisi');
            return;
        }

        this.saveUser(user);
    },

    confirmDeleteUser(uid, name) {
        this.state.selectedUserForDelete = { uid, name };
        const label = document.getElementById('delete-user-label');
        if (label) label.textContent = `${name} (UID: ${uid})`;
        this.state.modals.deleteUser = true;
        this.render();
    },

    executeDeleteUser() {
        if (this.state.selectedUserForDelete) {
            this.deleteUser(this.state.selectedUserForDelete.uid, this.state.selectedUserForDelete.name);
        }
    },

    confirmDeleteTemplate(uid, fid) {
        this.state.selectedTemplateForDelete = { uid, fid };
        const label = document.getElementById('delete-template-label');
        if (label) label.textContent = `UID: ${uid}, Finger: ${fid} (${this.getFingerName(fid)})`;
        this.state.modals.deleteTemplate = true;
        this.render();
    },

    executeDeleteTemplate() {
        if (this.state.selectedTemplateForDelete) {
            this.deleteTemplate(this.state.selectedTemplateForDelete.uid, this.state.selectedTemplateForDelete.fid);
        }
    },

    escapeHTML(str) {
        if (!str) return '';
        return String(str).replace(/[&<>'"]/g, tag => ({
            '&': '&amp;',
            '<': '&lt;',
            '>': '&gt;',
            "'": '&#39;',
            '"': '&quot;'
        }[tag] || tag));
    }
};

// Global entry point on DOM ready
document.addEventListener('DOMContentLoaded', () => {
    App.init();
});
