import './input.css';

// Fingerku - Tailwind CSS v4 Frontend Application (Consumes REST API v1)
const App = {
    state: {
        activeTab: 'dashboard',
        theme: 'system', // 'light', 'dark', 'system'
        connected: false,
        config: { ip: '192.168.20.77', port: 4370, password: 0, udp: false, omit_ping: false, auto_connect: true },
        info: null,
        stats: { total_records: 0, total_users: 0, total_enrolled_users: 0, today_records: 0, today_unique_users: 0, status_counts: {} },
        users: [],
        filteredUsers: [],
        templates: [],
        attendance: [],
        attendanceTotal: 0,
        attendancePage: 1,
        attendanceLimit: 25,
        livePunches: [],
        lastPunch: null,
        history: [],
        sseConnected: false
    },

    async init() {
        this.initTheme();

        // Close dropdowns on outside click
        document.addEventListener('click', (e) => {
            if (!e.target.closest('.group-dropdown-container')) {
                this.closeAllDropdowns();
            }
        });

        // Close on Escape key
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') {
                this.closeAllDropdowns();
                this.closeConfigModal();
                this.closeUserModal();
                const bioModal = document.getElementById('modal-biometrics');
                if (bioModal) bioModal.classList.add('hidden');
            }
        });

        // Safety timeout to ensure loading screen always disappears
        const safetyTimeout = setTimeout(() => this.hidePreloader(), 2500);

        try {
            // Concurrently fetch initial critical data
            await Promise.allSettled([
                this.loadStatus(),
                this.loadDeviceInfo(),
                this.loadAttendanceStats(),
                this.loadUsers(),
                this.loadAttendance(1)
            ]);
        } catch (err) {
            console.error('Initial load error:', err);
        } finally {
            clearTimeout(safetyTimeout);
            this.hidePreloader();
        }

        // Initialize Live Server-Sent Events
        this.initSSE();

        // Polling status every 10s
        setInterval(() => {
            this.loadStatus();
            this.loadAttendanceStats();
        }, 10000);
    },

    hidePreloader() {
        const preloader = document.getElementById('app-preloader');
        if (preloader && !preloader.classList.contains('opacity-0')) {
            preloader.classList.add('opacity-0', 'pointer-events-none');
            setTimeout(() => {
                preloader.classList.add('hidden');
            }, 350);
        }
    },

    // ----------------------------------------------------
    // THEME MANAGEMENT (Light, Dark, System)
    // ----------------------------------------------------
    initTheme() {
        const savedTheme = localStorage.getItem('fingerku-theme') || 'system';
        this.setTheme(savedTheme, false);

        // Listen for OS system theme changes
        window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', (e) => {
            if (this.state.theme === 'system') {
                if (e.matches) {
                    document.documentElement.classList.add('dark');
                } else {
                    document.documentElement.classList.remove('dark');
                }
            }
        });
    },

    setTheme(theme, save = true) {
        this.state.theme = theme;
        if (save) {
            localStorage.setItem('fingerku-theme', theme);
        }

        const isDark = theme === 'dark' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);
        if (isDark) {
            document.documentElement.classList.add('dark');
        } else {
            document.documentElement.classList.remove('dark');
        }

        // Update Theme Button Active States
        ['light', 'dark', 'system'].forEach(t => {
            const btn = document.getElementById(`theme-btn-${t}`);
            if (btn) {
                if (t === theme) {
                    btn.className = 'p-1.5 rounded-lg bg-white dark:bg-slate-700 text-emerald-600 dark:text-emerald-400 shadow-sm transition-all cursor-pointer';
                } else {
                    btn.className = 'p-1.5 rounded-lg text-slate-500 dark:text-slate-400 hover:text-slate-900 dark:hover:text-white transition-all cursor-pointer';
                }
            }
        });
    },

    // ----------------------------------------------------
    // GROUPED NAVIGATION DROPDOWNS
    // ----------------------------------------------------
    toggleDropdown(groupId) {
        const targetMenu = document.getElementById(`dropdown-${groupId}`);
        const targetBtn = document.getElementById(`group-btn-${groupId}`);
        const isHidden = targetMenu ? targetMenu.classList.contains('hidden') : true;

        // Close all other dropdowns
        this.closeAllDropdowns();

        if (isHidden && targetMenu && targetBtn) {
            targetMenu.classList.remove('hidden');
            const chevron = targetBtn.querySelector('.dropdown-chevron');
            if (chevron) chevron.classList.add('rotate-180');
        }
    },

    closeAllDropdowns() {
        document.querySelectorAll('.dropdown-menu').forEach(m => m.classList.add('hidden'));
        document.querySelectorAll('.dropdown-chevron').forEach(c => c.classList.remove('rotate-180'));
    },

    setTab(tabId) {
        this.state.activeTab = tabId;
        this.closeAllDropdowns();

        // Group association
        const tabGroups = {
            'dashboard': 'monitoring',
            'live': 'monitoring',
            'users': 'master',
            'templates': 'master',
            'attendance': 'reports',
            'history': 'reports',
            'hardware': 'device'
        };
        const activeGroup = tabGroups[tabId] || 'monitoring';

        // Update Group Header Buttons
        ['monitoring', 'master', 'reports', 'device'].forEach(grp => {
            const btn = document.getElementById(`group-btn-${grp}`);
            if (btn) {
                if (grp === activeGroup) {
                    btn.className = 'group-btn flex items-center gap-2 px-3.5 py-1.5 rounded-xl text-xs font-bold transition-all text-emerald-600 dark:text-emerald-400 bg-emerald-500/10 border border-emerald-500/30 cursor-pointer shadow-sm';
                } else {
                    btn.className = 'group-btn flex items-center gap-2 px-3.5 py-1.5 rounded-xl text-xs font-semibold transition-all text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-white hover:bg-slate-200/60 dark:hover:bg-slate-800/60 border border-transparent cursor-pointer';
                }
            }
        });

        // Update Sub-Item active states
        ['dashboard', 'live', 'users', 'templates', 'attendance', 'history', 'hardware'].forEach(id => {
            const item = document.getElementById(`menu-item-${id}`);
            if (item) {
                const isActive = id === tabId;
                item.classList.toggle('bg-emerald-500/10', isActive);
                item.classList.toggle('dark:bg-emerald-500/10', isActive);
            }
        });

        // Hide all views, show active
        ['dashboard', 'live', 'users', 'templates', 'attendance', 'hardware', 'history'].forEach(id => {
            const el = document.getElementById(`view-${id}`);
            if (el) el.classList.toggle('hidden', id !== tabId);
        });

        // Lazy load tab data
        if (tabId === 'templates' && this.state.templates.length === 0) this.loadTemplates();
        if (tabId === 'history' && this.state.history.length === 0) this.loadSyncHistory();
    },

    // ----------------------------------------------------
    // API CALLS
    // ----------------------------------------------------
    async loadStatus() {
        try {
            const res = await fetch('/api/v1/status');
            const result = await res.json();
            if (result.success && result.data) {
                this.state.connected = result.data.connected;
                this.state.config.ip = result.data.device_ip;
                this.state.config.port = result.data.device_port;
                this.renderHeaderStatus();
            }
        } catch (e) {
            console.error('Failed to load status:', e);
        }
    },

    async loadDeviceInfo() {
        try {
            const res = await fetch('/api/v1/device/info');
            const result = await res.json();
            if (result.success && result.data) {
                this.state.info = result.data.info;
                this.renderDeviceInfo(result.data);
            }
        } catch (e) {
            console.error('Failed to load device info:', e);
        }
    },

    async loadAttendanceStats() {
        try {
            const res = await fetch('/api/v1/attendance/stats');
            const result = await res.json();
            if (result.success && result.data) {
                this.state.stats = result.data;
                this.renderStats();
            }
        } catch (e) {
            console.error('Failed to load stats:', e);
        }
    },

    async loadUsers() {
        try {
            const res = await fetch('/api/v1/users');
            const result = await res.json();
            if (result.success && result.data) {
                this.state.users = result.data;
                this.filterUsers();
            }
        } catch (e) {
            console.error('Failed to load users:', e);
        }
    },

    async loadTemplates() {
        try {
            const res = await fetch('/api/v1/templates');
            const result = await res.json();
            if (result.success && result.data) {
                this.state.templates = result.data;
                this.renderTemplates();
            }
        } catch (e) {
            console.error('Failed to load templates:', e);
        }
    },

    async loadAttendance(page = 1) {
        this.state.attendancePage = page;
        const qSearch = document.getElementById('att-search')?.value.trim() || '';
        const qFrom = document.getElementById('att-from')?.value || '';
        const qTo = document.getElementById('att-to')?.value || '';

        const params = new URLSearchParams({
            page: page,
            limit: this.state.attendanceLimit
        });
        if (qSearch) params.append('search', qSearch);
        if (qFrom) params.append('from', qFrom);
        if (qTo) params.append('to', qTo);

        try {
            const res = await fetch(`/api/v1/attendance?${params.toString()}`);
            const result = await res.json();
            if (result.success) {
                this.state.attendance = result.data || [];
                this.state.attendanceTotal = result.total || 0;
                this.renderAttendanceTable();
            }
        } catch (e) {
            console.error('Failed to load attendance:', e);
        }
    },

    async loadSyncHistory() {
        try {
            const res = await fetch('/api/v1/sync/history?limit=20');
            const result = await res.json();
            if (result.success && result.data) {
                this.state.history = result.data;
                this.renderSyncHistory();
            }
        } catch (e) {
            console.error('Failed to load sync history:', e);
        }
    },

    async triggerSync() {
        const btn = document.getElementById('btn-header-sync');
        const icon = document.getElementById('icon-header-sync');
        if (icon) icon.classList.add('animate-spin');
        if (btn) btn.disabled = true;

        this.showToast('info', 'Sinkronisasi Dimulai', 'Mengambil user, template sidik jari & log presensi...');

        try {
            const res = await fetch('/api/v1/sync', { method: 'POST' });
            const result = await res.json();
            if (result.success) {
                const d = result.data;
                this.showToast('success', 'Sinkronisasi Selesai', 
                    `Tersinkron: ${d.total_users} User, ${d.saved_templates} Jari, ${d.new_records} Log Baru`);
                this.loadStatus();
                this.loadDeviceInfo();
                this.loadAttendanceStats();
                this.loadUsers();
                this.loadAttendance(1);
            } else {
                throw new Error(result.error || 'Gagal sinkronisasi');
            }
        } catch (e) {
            this.showToast('error', 'Gagal Sinkronisasi', e.message);
        } finally {
            if (icon) icon.classList.remove('animate-spin');
            if (btn) btn.disabled = false;
        }
    },

    // ----------------------------------------------------
    // HARDWARE CONTROLS
    // ----------------------------------------------------
    async triggerUnlock() {
        const secInput = document.getElementById('ctrl-unlock-sec');
        const sec = parseInt(secInput?.value) || 5;

        try {
            const res = await fetch('/api/v1/device/unlock', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ seconds: sec })
            });
            const result = await res.json();
            if (result.success) {
                this.showToast('success', 'Pintu Terbuka', `Relay kunci pintu terbuka selama ${sec} detik.`);
            } else {
                throw new Error(result.error || 'Gagal membuka relay');
            }
        } catch (e) {
            this.showToast('error', 'Gagal Relay Pintu', e.message);
        }
    },

    async triggerSyncTime() {
        try {
            const res = await fetch('/api/v1/device/synctime', { method: 'POST' });
            const result = await res.json();
            if (result.success) {
                this.showToast('success', 'Jam Disinkronkan', `Waktu RTC mesin berhasil diset: ${result.data?.time}`);
                this.loadDeviceInfo();
            } else {
                throw new Error(result.error || 'Gagal sinkronisasi jam');
            }
        } catch (e) {
            this.showToast('error', 'Gagal Waktu RTC', e.message);
        }
    },

    async triggerVoice() {
        const idxSelect = document.getElementById('ctrl-voice-idx');
        const idx = parseInt(idxSelect?.value) || 0;

        try {
            const res = await fetch('/api/v1/device/voice', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ index: idx })
            });
            const result = await res.json();
            if (result.success) {
                this.showToast('success', 'Suara Diputar', `Memutar prompt suara index ${idx}`);
            } else {
                throw new Error(result.error || 'Gagal memutar suara');
            }
        } catch (e) {
            this.showToast('error', 'Gagal Audio Speaker', e.message);
        }
    },

    async triggerRestart() {
        if (!confirm('Apakah Anda yakin ingin me-restart / reboot mesin ZKTeco?')) return;
        try {
            const res = await fetch('/api/v1/device/restart', { method: 'POST' });
            const result = await res.json();
            if (result.success) {
                this.showToast('warning', 'Perangkat Rebooting', 'Perintah reboot berhasil dikirim.');
                this.loadStatus();
            } else {
                throw new Error(result.error || 'Gagal reboot');
            }
        } catch (e) {
            this.showToast('error', 'Gagal Reboot', e.message);
        }
    },

    async triggerPowerOff() {
        if (!confirm('Apakah Anda yakin ingin mematikan (Shutdown) mesin ZKTeco?')) return;
        try {
            const res = await fetch('/api/v1/device/poweroff', { method: 'POST' });
            const result = await res.json();
            if (result.success) {
                this.showToast('warning', 'Perangkat Dimatikan', 'Perintah power off berhasil dikirim.');
                this.loadStatus();
            } else {
                throw new Error(result.error || 'Gagal power off');
            }
        } catch (e) {
            this.showToast('error', 'Gagal Power Off', e.message);
        }
    },

    // ----------------------------------------------------
    // REAL-TIME SSE STREAM
    // ----------------------------------------------------
    initSSE() {
        if (!!window.EventSource) {
            const source = new EventSource('/api/v1/events');
            
            source.addEventListener('punch', (e) => {
                try {
                    const data = JSON.parse(e.data);
                    this.handleLivePunch(data);
                } catch (err) {
                    console.error('SSE Punch Parse error:', err);
                }
            });

            source.onerror = () => {
                source.close();
                setTimeout(() => this.initSSE(), 5000);
            };
        }
    },

    handleLivePunch(punch) {
        this.state.lastPunch = punch;
        this.state.livePunches.unshift(punch);
        if (this.state.livePunches.length > 50) this.state.livePunches.pop();

        this.renderLiveKiosk();
        this.loadAttendanceStats();
        this.showToast('success', `Presensi: ${punch.user_name}`, `${punch.status_name} pada ${new Date(punch.timestamp).toLocaleTimeString()}`);
    },

    // ----------------------------------------------------
    // RENDERING HELPERS
    // ----------------------------------------------------
    renderHeaderStatus() {
        const badge = document.getElementById('header-status-badge');
        const text = document.getElementById('header-status-text');
        const dbBadge = document.getElementById('header-db-records');

        if (badge && text) {
            if (this.state.connected) {
                badge.className = 'flex items-center gap-2 px-3 py-1.5 rounded-full text-xs font-semibold bg-emerald-500/10 text-emerald-400 border border-emerald-500/30';
                badge.innerHTML = `
                    <span class="relative flex h-2 w-2">
                        <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                        <span class="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
                    </span>
                    <span>${this.state.config.ip}:${this.state.config.port}</span>
                `;
            } else {
                badge.className = 'flex items-center gap-2 px-3 py-1.5 rounded-full text-xs font-semibold bg-rose-500/10 text-rose-400 border border-rose-500/30';
                badge.innerHTML = `
                    <span class="inline-flex rounded-full h-2 w-2 bg-rose-500"></span>
                    <span>TERPUTUS</span>
                `;
            }
        }

        if (dbBadge) {
            dbBadge.textContent = `${this.state.stats.total_records.toLocaleString()} Rekaman SQLite`;
        }
    },

    renderStats() {
        const s = this.state.stats;
        const enrolledUsers = s.total_enrolled_users || (this.state.users ? this.state.users.length : 0);
        document.getElementById('kpi-total-records').textContent = s.total_records.toLocaleString();
        document.getElementById('kpi-today-records').textContent = s.today_records.toLocaleString();
        document.getElementById('kpi-today-users').textContent = `(${s.today_unique_users} Pegawai Unik)`;
        document.getElementById('kpi-total-users').textContent = enrolledUsers.toLocaleString();
        const subEl = document.getElementById('kpi-total-users-sub');
        if (subEl) {
            subEl.textContent = `User di Mesin (${s.total_users} ID di Log)`;
        }
        document.getElementById('kpi-total-templates').textContent = this.state.templates.length || this.state.info?.sizes?.fingers || 0;
        document.getElementById('att-total-badge').textContent = `${s.total_records.toLocaleString()} Total Log`;

        // Breakdown container
        const container = document.getElementById('status-counts-container');
        if (container && s.status_counts) {
            const entries = Object.entries(s.status_counts);
            if (entries.length === 0) {
                container.innerHTML = `<div class="text-slate-500 text-center py-4">Belum ada log presensi</div>`;
            } else {
                container.innerHTML = entries.map(([name, count]) => {
                    const pct = s.total_records > 0 ? ((count / s.total_records) * 100).toFixed(1) : 0;
                    return `
                        <div>
                            <div class="flex justify-between text-xs mb-1">
                                <span class="text-slate-300 font-semibold">${name}</span>
                                <span class="text-slate-400">${count.toLocaleString()} (${pct}%)</span>
                            </div>
                            <div class="w-full h-1.5 bg-slate-800 rounded-full overflow-hidden">
                                <div class="h-full bg-cyan-500 rounded-full" style="width: ${pct}%"></div>
                            </div>
                        </div>
                    `;
                }).join('');
            }
        }
    },

    renderDeviceInfo(data) {
        if (!data || !data.info) return;
        const info = data.info;
        
        document.getElementById('dev-name').textContent = info.device_name || 'ZKTeco Terminal';
        document.getElementById('dev-platform').textContent = info.platform || '-';
        document.getElementById('dev-serial').textContent = info.serial_number || '-';
        document.getElementById('dev-firmware').textContent = info.firmware_version || '-';
        document.getElementById('dev-mac').textContent = info.mac || '-';
        document.getElementById('dev-time').textContent = data.device_time || '-';

        if (info.sizes) {
            const recPct = info.sizes.records_capacity > 0 ? (info.sizes.records / info.sizes.records_capacity) * 100 : 0;
            document.getElementById('cap-records-label').textContent = `${info.sizes.records.toLocaleString()} / ${info.sizes.records_capacity.toLocaleString()}`;
            document.getElementById('cap-records-bar').style.width = `${recPct}%`;

            const fingPct = info.sizes.fingers_capacity > 0 ? (info.sizes.fingers / info.sizes.fingers_capacity) * 100 : 0;
            document.getElementById('cap-fingers-label').textContent = `${info.sizes.fingers.toLocaleString()} / ${info.sizes.fingers_capacity.toLocaleString()}`;
            document.getElementById('cap-fingers-bar').style.width = `${fingPct}%`;
            document.getElementById('kpi-total-templates').textContent = info.sizes.fingers;
        }
    },

    renderLiveKiosk() {
        const last = this.state.lastPunch;
        const hero = document.getElementById('kiosk-hero-card');

        if (hero && last) {
            const isCheckIn = last.status === 0;
            hero.className = `glass-panel p-8 rounded-3xl border transition-all duration-500 ${isCheckIn ? 'border-emerald-500/40 glow-emerald' : 'border-amber-500/40 glow-cyan'}`;
            hero.innerHTML = `
                <div class="flex flex-col md:flex-row items-center justify-center gap-6">
                    <div class="w-24 h-24 rounded-2xl bg-gradient-to-br from-emerald-500 to-teal-600 flex items-center justify-center text-white text-3xl font-black shadow-xl shadow-emerald-500/30">
                        ${last.user_name ? last.user_name.substring(0,2).toUpperCase() : 'US'}
                    </div>
                    <div class="text-center md:text-left">
                        <div class="inline-flex items-center gap-2 px-3 py-1 rounded-full text-xs font-bold uppercase tracking-wider mb-2 ${isCheckIn ? 'bg-emerald-500 text-emerald-950' : 'bg-amber-500 text-amber-950'}">
                            ${last.status_name} TERVERIFIKASI
                        </div>
                        <h2 class="text-3xl font-extrabold text-white tracking-tight">${last.user_name}</h2>
                        <div class="text-slate-300 font-mono mt-1 flex flex-wrap gap-4 justify-center md:justify-start text-sm">
                            <span>User ID: <strong class="text-white">${last.user_id}</strong></span>
                            <span>UID: <strong class="text-white">${last.uid}</strong></span>
                            <span>Waktu: <strong class="text-emerald-400">${new Date(last.timestamp).toLocaleTimeString()}</strong></span>
                        </div>
                    </div>
                </div>
            `;
        }

        const tbody = document.getElementById('live-stream-table-body');
        if (tbody) {
            if (this.state.livePunches.length === 0) {
                tbody.innerHTML = `<tr><td colspan="5" class="py-8 text-center text-slate-500">Belum ada data tap pada sesi ini</td></tr>`;
            } else {
                tbody.innerHTML = this.state.livePunches.map((p, idx) => `
                    <tr class="border-b border-slate-800/60 hover:bg-slate-800/40 transition-colors ${idx === 0 ? 'bg-emerald-500/5' : ''}">
                        <td class="py-3 px-4 font-mono font-bold text-emerald-400">${new Date(p.timestamp).toLocaleTimeString()}</td>
                        <td class="py-3 px-4 font-mono font-bold text-slate-200">${p.user_id}</td>
                        <td class="py-3 px-4 font-semibold text-white">${p.user_name}</td>
                        <td class="py-3 px-4">
                            <span class="px-2.5 py-0.5 rounded-full text-xs font-bold ${p.status === 0 ? 'bg-emerald-500/20 text-emerald-300 border border-emerald-500/40' : 'bg-amber-500/20 text-amber-300 border border-amber-500/40'}">
                                ${p.status_name}
                            </span>
                        </td>
                        <td class="py-3 px-4 font-mono text-xs text-slate-400">UID: ${p.uid} (Punch ${p.punch})</td>
                    </tr>
                `).join('');
            }
        }
    },

    filterUsers() {
        const query = document.getElementById('user-search-input')?.value.toLowerCase().trim() || '';
        const role = document.getElementById('user-role-filter')?.value || 'all';

        this.state.filteredUsers = this.state.users.filter(u => {
            const matchesQuery = !query || 
                (u.name && u.name.toLowerCase().includes(query)) ||
                (u.user_id && u.user_id.toLowerCase().includes(query)) ||
                String(u.uid).includes(query);

            const isRoleAdmin = u.privilege === 14 || u.role === 'Admin';
            const isRoleManager = u.privilege === 2 || u.role === 'Manager';
            const isRoleUser = !isRoleAdmin && !isRoleManager;

            let matchesRole = true;
            if (role === 'admin') matchesRole = isRoleAdmin;
            else if (role === 'manager') matchesRole = isRoleManager;
            else if (role === 'user') matchesRole = isRoleUser;

            return matchesQuery && matchesRole;
        });

        document.getElementById('users-count-badge').textContent = `${this.state.filteredUsers.length} User`;
        this.renderUsersTable();
    },

    renderUsersTable() {
        const tbody = document.getElementById('users-table-body');
        if (!tbody) return;

        if (this.state.filteredUsers.length === 0) {
            tbody.innerHTML = `<tr><td colspan="7" class="py-12 text-center text-slate-500">Tidak ada pengguna yang cocok</td></tr>`;
            return;
        }

        tbody.innerHTML = this.state.filteredUsers.map(u => {
            const fCount = u.finger_count || 0;
            const role = u.role || (u.privilege === 14 ? 'Admin' : 'User');
            return `
                <tr class="border-b border-slate-200 dark:border-slate-800/60 hover:bg-slate-100/70 dark:hover:bg-slate-800/40 transition-colors">
                    <td class="py-3.5 px-4 font-mono text-slate-500 dark:text-slate-400">${u.uid}</td>
                    <td class="py-3.5 px-4 font-mono font-bold text-emerald-600 dark:text-emerald-400">${u.user_id}</td>
                    <td class="py-3.5 px-4 font-semibold text-slate-900 dark:text-slate-100">${u.name}</td>
                    <td class="py-3.5 px-4">
                        <button onclick="App.viewUserBiometrics('${u.user_id}')" class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs font-mono font-semibold ${fCount > 0 ? 'bg-amber-500/10 text-amber-700 dark:text-amber-300 border border-amber-500/30 hover:bg-amber-500/20' : 'bg-slate-200 dark:bg-slate-800 text-slate-500'} cursor-pointer transition-all">
                            <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 11c0 3.517-1.009 6.799-2.753 9.571m-3.44-2.04l.054-.09A13.916 13.916 0 008 11a4 4 0 118 0c0 1.017-.07 2.019-.203 3m-2.118 6.844A21.88 21.88 0 0015.171 17m3.839 1.132c.645-2.266.99-4.659.99-7.132A8 8 0 004.07 3.755"/></svg>
                            <span>${fCount > 0 ? `${fCount} Sidik Jari` : 'Belum Ada'}</span>
                        </button>
                    </td>
                    <td class="py-3.5 px-4">
                        <span class="px-2.5 py-0.5 rounded-full text-xs font-bold ${role === 'Admin' ? 'bg-rose-500/20 text-rose-700 dark:text-rose-300 border border-rose-500/40' : 'bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300'}">
                            ${role}
                        </span>
                    </td>
                    <td class="py-3.5 px-4 font-mono text-slate-500 dark:text-slate-400">${u.card > 0 ? u.card : '-'}</td>
                    <td class="py-3.5 px-4 text-right">
                        <div class="flex items-center justify-end gap-1.5">
                            <button onclick="App.deleteUser('${u.user_id}', '${u.name}')" class="p-1.5 rounded-lg bg-rose-100 dark:bg-rose-950/40 hover:bg-rose-200 dark:hover:bg-rose-900/60 text-rose-600 dark:text-rose-400 border border-rose-300 dark:border-rose-800/60 transition-colors cursor-pointer" title="Hapus User">
                                <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
                            </button>
                        </div>
                    </td>
                </tr>
            `;
        }).join('');
    },

    renderTemplates() {
        const tbody = document.getElementById('templates-table-body');
        if (!tbody) return;
        document.getElementById('templates-count-badge').textContent = `${this.state.templates.length} Template`;

        if (this.state.templates.length === 0) {
            tbody.innerHTML = `<tr><td colspan="5" class="py-8 text-center text-slate-500">Belum ada data template biometrik di database</td></tr>`;
            return;
        }

        tbody.innerHTML = this.state.templates.map(t => `
            <tr class="border-b border-slate-200 dark:border-slate-800/60 hover:bg-slate-100/70 dark:hover:bg-slate-800/40 transition-colors">
                <td class="py-3 px-4 font-bold text-emerald-600 dark:text-emerald-400">${t.uid}</td>
                <td class="py-3 px-4 font-bold text-amber-600 dark:text-amber-400">FID ${t.fid}</td>
                <td class="py-3 px-4 text-slate-700 dark:text-slate-300">${t.valid === 1 ? 'Valid (1)' : 'Non-Valid'}</td>
                <td class="py-3 px-4 text-cyan-600 dark:text-cyan-400">${t.size} bytes</td>
                <td class="py-3 px-4 text-slate-500 text-xs truncate max-w-xs">Biometric BLOB (${t.size}B)</td>
            </tr>
        `).join('');
    },

    renderAttendanceTable() {
        const tbody = document.getElementById('att-table-body');
        if (!tbody) return;

        if (this.state.attendance.length === 0) {
            tbody.innerHTML = `<tr><td colspan="7" class="py-12 text-center text-slate-500">Tidak ada log presensi ditemukan</td></tr>`;
            return;
        }

        tbody.innerHTML = this.state.attendance.map(a => {
            const isCheckIn = a.status === 0;
            return `
                <tr class="border-b border-slate-200 dark:border-slate-800/60 hover:bg-slate-100/70 dark:hover:bg-slate-800/40 transition-colors">
                    <td class="py-3 px-4 font-mono text-slate-500">${a.id}</td>
                    <td class="py-3 px-4 font-mono font-bold text-emerald-600 dark:text-emerald-400">${a.user_id}</td>
                    <td class="py-3 px-4 font-semibold text-slate-900 dark:text-slate-100">${a.user_name || '-'}</td>
                    <td class="py-3 px-4 font-mono text-slate-700 dark:text-slate-300">${new Date(a.timestamp).toLocaleString()}</td>
                    <td class="py-3 px-4">
                        <span class="px-2.5 py-0.5 rounded-full text-xs font-bold ${isCheckIn ? 'bg-emerald-500/20 text-emerald-700 dark:text-emerald-300 border border-emerald-500/40' : 'bg-amber-500/20 text-amber-700 dark:text-amber-300 border border-amber-500/40'}">
                            ${a.status_name}
                        </span>
                    </td>
                    <td class="py-3 px-4 font-mono text-slate-600 dark:text-slate-400">${a.punch}</td>
                    <td class="py-3 px-4 font-mono text-xs text-slate-500">${a.source} (${a.device_ip})</td>
                </tr>
            `;
        }).join('');

        // Pagination controls
        const totalPages = Math.ceil(this.state.attendanceTotal / this.state.attendanceLimit) || 1;
        document.getElementById('att-page-info').textContent = `Halaman ${this.state.attendancePage} dari ${totalPages} (${this.state.attendanceTotal.toLocaleString()} Data)`;
        document.getElementById('btn-att-prev').disabled = this.state.attendancePage <= 1;
        document.getElementById('btn-att-next').disabled = this.state.attendancePage >= totalPages;
    },

    renderSyncHistory() {
        const tbody = document.getElementById('history-table-body');
        if (!tbody) return;

        if (this.state.history.length === 0) {
            tbody.innerHTML = `<tr><td colspan="6" class="py-8 text-center text-slate-500">Belum ada riwayat audit sinkronisasi</td></tr>`;
            return;
        }

        tbody.innerHTML = this.state.history.map(h => `
            <tr class="border-b border-slate-200 dark:border-slate-800/60 hover:bg-slate-100/70 dark:hover:bg-slate-800/40 transition-colors">
                <td class="py-3 px-4 text-slate-500">#${h.id}</td>
                <td class="py-3 px-4 font-bold text-emerald-600 dark:text-emerald-400">${h.device_ip}</td>
                <td class="py-3 px-4 text-slate-700 dark:text-slate-300">${new Date(h.synced_at).toLocaleString()}</td>
                <td class="py-3 px-4 text-slate-800 dark:text-slate-200">${h.total_records.toLocaleString()}</td>
                <td class="py-3 px-4 font-bold text-cyan-600 dark:text-cyan-400">+${h.new_records.toLocaleString()}</td>
                <td class="py-3 px-4">
                    <span class="px-2 py-0.5 rounded-full text-xs font-bold ${h.status === 'success' ? 'bg-emerald-500/20 text-emerald-700 dark:text-emerald-400 border border-emerald-500/30' : 'bg-rose-500/20 text-rose-700 dark:text-rose-400 border border-rose-500/30'}">
                        ${h.status.toUpperCase()}
                    </span>
                </td>
            </tr>
        `).join('');
    },

    // ----------------------------------------------------
    // MODALS & USER ACTIONS
    // ----------------------------------------------------
    openConfigModal() {
        document.getElementById('cfg-ip').value = this.state.config.ip || '192.168.20.77';
        document.getElementById('cfg-port').value = this.state.config.port || 4370;
        document.getElementById('cfg-password').value = this.state.config.password || 0;
        document.getElementById('cfg-udp').checked = !!this.state.config.udp;
        document.getElementById('cfg-omit-ping').checked = !!this.state.config.omit_ping;
        document.getElementById('cfg-auto-connect').checked = !!this.state.config.auto_connect;
        document.getElementById('modal-config').classList.remove('hidden');
    },

    closeConfigModal() {
        document.getElementById('modal-config').classList.add('hidden');
    },

    async saveConfig() {
        const payload = {
            ip: document.getElementById('cfg-ip').value.trim(),
            port: parseInt(document.getElementById('cfg-port').value) || 4370,
            password: parseInt(document.getElementById('cfg-password').value) || 0,
            udp: document.getElementById('cfg-udp').checked,
            omit_ping: document.getElementById('cfg-omit-ping').checked,
            auto_connect: document.getElementById('cfg-auto-connect').checked,
            auto_sync_interval_sec: 0
        };

        try {
            const res = await fetch('/api/v1/config', {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload)
            });
            const result = await res.json();
            if (result.success) {
                this.state.config = payload;
                this.closeConfigModal();
                this.showToast('success', 'Konfigurasi Disimpan', 'Menghubungkan ke mesin dengan konfigurasi baru...');
                this.loadStatus();
                this.loadDeviceInfo();
            } else {
                throw new Error(result.error || 'Gagal menyimpan konfigurasi');
            }
        } catch (e) {
            this.showToast('error', 'Gagal Konfigurasi', e.message);
        }
    },

    openAddUserModal() {
        document.getElementById('user-modal-title').textContent = 'Tambah Pengguna Baru';
        document.getElementById('usr-id').value = '';
        document.getElementById('usr-uid').value = '';
        document.getElementById('usr-name').value = '';
        document.getElementById('usr-privilege').value = '0';
        document.getElementById('usr-card').value = '0';
        document.getElementById('modal-user').classList.remove('hidden');
    },

    closeUserModal() {
        document.getElementById('modal-user').classList.add('hidden');
    },

    async saveUser() {
        const payload = {
            user_id: document.getElementById('usr-id').value.trim(),
            uid: parseInt(document.getElementById('usr-uid').value) || 0,
            name: document.getElementById('usr-name').value.trim(),
            privilege: parseInt(document.getElementById('usr-privilege').value) || 0,
            card: parseInt(document.getElementById('usr-card').value) || 0,
            password: '',
            group_id: '1'
        };

        try {
            const res = await fetch('/api/v1/users', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload)
            });
            const result = await res.json();
            if (result.success) {
                this.showToast('success', 'User Disimpan', `Pengguna ${payload.name} berhasil disimpan ke mesin.`);
                this.closeUserModal();
                this.loadUsers();
            } else {
                throw new Error(result.error || 'Gagal menyimpan user');
            }
        } catch (e) {
            this.showToast('error', 'Gagal Simpan User', e.message);
        }
    },

    async deleteUser(userId, name) {
        if (!confirm(`Apakah Anda yakin ingin menghapus user "${name}" (ID: ${userId}) dari mesin dan SQLite?`)) return;

        try {
            const res = await fetch(`/api/v1/users/${userId}`, { method: 'DELETE' });
            const result = await res.json();
            if (result.success) {
                this.showToast('success', 'User Dihapus', `User ${name} berhasil dihapus.`);
                this.loadUsers();
            } else {
                throw new Error(result.error || 'Gagal menghapus user');
            }
        } catch (e) {
            this.showToast('error', 'Gagal Hapus User', e.message);
        }
    },

    async viewUserBiometrics(userId) {
        try {
            const res = await fetch(`/api/v1/users/${userId}`);
            const result = await res.json();
            if (result.success && result.data) {
                const u = result.data.user;
                const tmps = result.data.templates || [];
                
                document.getElementById('bio-user-subtitle').textContent = `${u.name} (User ID: ${u.user_id}, UID: ${u.uid})`;
                const container = document.getElementById('bio-templates-list');
                
                if (tmps.length === 0) {
                    container.innerHTML = `<div class="text-center py-6 text-slate-500">User ini belum memiliki template sidik jari biometrik terdaftar.</div>`;
                } else {
                    container.innerHTML = tmps.map(t => `
                        <div class="p-3.5 rounded-xl bg-slate-900/80 border border-slate-800 flex items-center justify-between font-mono text-xs">
                            <div class="flex items-center gap-3">
                                <div class="w-8 h-8 rounded-lg bg-amber-500/10 text-amber-400 flex items-center justify-center font-bold">
                                    ${t.fid}
                                </div>
                                <div>
                                    <div class="font-bold text-white">Finger Index (FID) ${t.fid}</div>
                                    <div class="text-slate-400 text-[10px]">Ukuran Template: ${t.size} bytes</div>
                                </div>
                            </div>
                            <span class="px-2 py-0.5 rounded-full text-[10px] font-bold bg-emerald-500/10 text-emerald-400 border border-emerald-500/30">
                                TERDAFTAR
                            </span>
                        </div>
                    `).join('');
                }

                document.getElementById('modal-biometrics').classList.remove('hidden');
            }
        } catch (e) {
            this.showToast('error', 'Gagal Biometrik', e.message);
        }
    },

    applyAttendanceFilter() {
        this.loadAttendance(1);
    },

    prevAttendancePage() {
        if (this.state.attendancePage > 1) {
            this.loadAttendance(this.state.attendancePage - 1);
        }
    },

    nextAttendancePage() {
        this.loadAttendance(this.state.attendancePage + 1);
    },

    exportAttendanceCSV() {
        window.open('/api/v1/attendance?limit=10000', '_blank');
    },

    showToast(type, title, message) {
        const container = document.getElementById('toast-container');
        if (!container) return;

        const colors = {
            success: 'bg-emerald-950/90 border-emerald-500/50 text-emerald-300',
            error: 'bg-rose-950/90 border-rose-500/50 text-rose-300',
            info: 'bg-cyan-950/90 border-cyan-500/50 text-cyan-300',
            warning: 'bg-amber-950/90 border-amber-500/50 text-amber-300'
        };

        const toast = document.createElement('div');
        toast.className = `p-4 rounded-2xl border backdrop-blur-xl shadow-2xl transition-all duration-300 pointer-events-auto transform translate-y-2 opacity-0 ${colors[type] || colors.info}`;
        toast.innerHTML = `
            <div class="font-bold text-xs">${title}</div>
            <div class="text-[11px] text-slate-300 mt-0.5 leading-snug">${message}</div>
        `;

        container.appendChild(toast);
        setTimeout(() => toast.classList.remove('translate-y-2', 'opacity-0'), 10);
        setTimeout(() => {
            toast.classList.add('opacity-0', 'translate-y-2');
            setTimeout(() => toast.remove(), 300);
        }, 4000);
    }
};

// Attach to global window for inline onclick handlers
window.App = App;

// Initialize App on DOM load
document.addEventListener('DOMContentLoaded', () => App.init());

export default App;
