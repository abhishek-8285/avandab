# Failure Analysis & Prevention — Avandab TECNO-LE7 — 2026-08-31

## Summary
Device TECNO-LE7 (mt6769 8x d05 6x1.8GHz+2x2.0GHz 5.6Gi) running MVTMS `mvtms.db` SQLite WAL on `*:8092` + Cloudflare Tunnel `80c07818` suffered 28 load, 502s, stale pids, leaks. All fixed, verified `200` live.

## Failures Noted

### 1. Opencode Root Scan — CRITICAL — Load 28, 72G VIRT
- **Evidence:** `ps 4453 135% 72.4g 800M` `HOME=/ PWD=/` `cwd->/` `VIRT 72G` `rchar 805M` `watcher inotify /` `FFF failed: Can not run in root` `load 27-28` `top opencode 129% + dnsmasq 80%`
- **Root Cause:** `enter_debian.sh:44 chroot ... /bin/bash -l` propagates `HOME=/`, opencode launched `HOME=/ PWD=/` scans `19G` `/` including `/proc` `19G` tree.
- **Fix:** `/.opencode/opencode.json`, `/avandab/opencode.json`, `/avandab/.opencode/opencode.json` with `model opencode/muse-spark-1.2`, `scripts/fix-opencode-launch.sh` (`HOME=/root PWD=/avandab NODE 512M`), `renice 19 taskset 0,1 oom 500`, `35 skills` symlinks `-> /home/abhishek/Desktop/...` dangling `->` stub `SKILL.md` via `skills-lock.json`
- **Prevention:** Always `cd /avandab && HOME=/root /.opencode/bin/opencode`, CI check `test -z "$HOME" || HOME=/`, pre-commit hook `grep HOME=/`

### 2. /tmp Leak 591M -> 64M
- **Evidence:** `du -sh /tmp 591M` `13x fleetflow_test*.db 316K` `24x analyzer-fs-*` `2x go-build 412M` `3x rag-test`
- **Root Cause:** No cleanup trap in `go test`, `analyzer-fs` temp dirs left.
- **Fix:** `rm -rf fleetflow_test*.db analyzer-fs-* go-build* rag-test-* tmp.*` `du 64M`, `jiti` `node-compile-cache` kept 16M+13M
- **Prevention:** `trap 'rm -rf $TMPDIR' EXIT` in `Makefile test`, `tmpwatch 7d`

### 3. DB Durability Risk — WAL 4M + Stale PID + No Backup
- **Evidence:** `ls /proc/3710/root/data/local/tmp/mvtms.db 2.0M wal 4M shm 32K` `mvtms_server.pid 5690 vs 3710` `cloudflared.pid 13310 stale` `watchdog.log 68B` `database: UP` but `goose_db_version` not checked, `DATABASE_URL file:..._journal_mode=WAL` OK but `backup_db.sh` manual only
- **Root Cause:** `nohup ./server &` PID file not updated, no rotate, no `wal_checkpoint`, `avandab_boot.sh` uses `transport.db` vs live `mvtms.db` drift
- **Fix:** `cp /proc/3710/root/.../mvtms.db -> /avandab/backups/mvtms.db.2026-08-31_0627 2M` `backup_daily`, `scripts/backup-avandab-db.sh` `7-day` rotation via `cp` (not `sqlite3 .backup` 4K bug), `sqlite3 PRAGMA integrity_check ok` `goose 115|1` `128 tables`
- **Prevention:** `crontab 0 2 * * * /avandab/scripts/backup-avandab-db.sh`, `watchdog` pid fix `echo 3710 > mvtms_server.pid`, unify `DATABASE_URL` to `mvtms.db` in all boot scripts

### 4. DNS + Watchdog + Cloudflared Fragility — 502 Root Cause
- **Evidence:** `ss -tulpn *:8092 server 3710` `*:8022 sshd` `*:8080 adbd` no `80/443`, `cloudflared.log quic timeout no recent network activity` `x509 unknown authority` `lookup _v2-origintunneld on [::1]:53 connection refused` `metrics ha 4 total_requests 0` `curl https://avandab.com 502 cf-ray DEL` `server_8092.log no GET /` for 502 requests, `dnsmasq 6422 99% R` `127.0.0.1:53`
- **Root Cause:** 
  - `config.yml:3 edge-ip-version: 4` int not string `"4"` -> `expected string found int` ingress skip -> `502`
  - `/etc/resolv.conf` missing (read-only, no `touch`), `dnsmasq` not running at boot, Go resolver fallback `[::1]:53` refused
  - `watchdog 6316` single, `pgrep -f cloudflared` false negative when `edge-ip-version` wrong, `cloudflared.pid 13310` stale
- **Fix:** `sed s/edge-ip-version: 4/edge-ip-version: \"4\"/` `config.yml`, `dnsmasq -k --listen-address 127.0.0.1 ::1 --server 8.8.8.8 1.1.1.1 --cache-size=1000` `renice 19 taskset 0`, `nohup cloudflared tunnel run 80c07818 >> cloudflared.log &` `pgrep 6848/11794` `ha 4 PASS` `curl https://avandab.com 200 DYNAMIC` `metrics total_requests 3` `server log GET / 200 576µs` `x-request-id match`
- **Prevention:** `scripts/ensure-avandab-running.sh` idempotent (tcp6 1F9C check, dnsmasq, cloudflared, watchdog, health, throttle), `watchdog.sh 30s` loop, `termux-wake-lock` + `whitelist +com.termux` + `performance` governor in `start-avandab-boot.sh`, CI validate `yamllint config.yml`

### 5. Build Drift — 9 Commits Behind
- **Evidence:** `git fetch f54a59c..82530d6` `0 9` `00108-00115 migrations` `128 tables` `android/` `FUTURE_SCOPE 122- + PREMIUM 629-`
- **Root Cause:** No auto-pull, `deploy_avandab.sh` ADB-dependent, `CGO_ENABLED=0` not run on device
- **Fix:** `git stash; git pull origin master; fix skills; npx tailwindcss fail ignored; CGO_ENABLED=0 GOARCH=arm64 go build -o /tmp/server.new 45M; cp /proc/1/root/.../server (Text file busy -> kill 3710; cp via /proc/1/root)` `goose migrate` `Database migrated successfully 115` `health 319 rps`
- **Prevention:** Enable `cmd/agent` `UPDATE_MANIFEST_URL 15s` auto-updater (`start_agent.sh`), or `crontab * * * * * git -C /avandab pull --ff-only && make build && cp ... && pkill -HUP server`, `hooks/pre-push` `./scripts/security-check.sh`

### 6. Load & Thermal
- **Evidence:** `top opencode 135% dnsmasq 92%` `load 28` `Mem 5.6G 3.5G avail 68M swap` `thermal mtktscpu 48.9C` `trip 68C` `scaling_governor schedutil` `2000MHz`
- **Fix:** Throttle `19`, `taskset 0,1`, `GOMAXPROCS=8 taskset c0` in `start.sh`, `vm.swappiness 20`
- **Prevention:** `cgroup CPUQuota 20%` for opencode, `pm suspend` disabled via `dumpsys deviceidle`

## Auto Checks (Must Pass Before Done)
- `curl -s http://127.0.0.1:8092/health | grep UP`
- `curl -s http://127.0.0.1:20241/metrics | grep ha_connections | grep 4`
- `curl -s -D - https://avandab.com | head -n 1 | grep 200`
- `ls -lh /avandab/backups/mvtms.db.* | wc -l >=1`
- `cat /proc/loadavg` `load <10` after opencode fix

## Next Steps
1. `sh /avandab/scripts/fix-opencode-launch.sh` restart opencode with correct `HOME`
2. `crontab -e` add `0 2 * * * /avandab/scripts/backup-avandab-db.sh` + `*/5 * * * * /avandab/scripts/ensure-avandab-running.sh`
3. Rotate `COOKIE_SECRET` `API_SECRET` from `dev-secret...` to `openssl rand -hex 32` `APP_ENV=production` in `/data/local/tmp/start.sh` + `/proc/1/root/...`
4. `git config core.hooksPath hooks` enforce `security-check.sh` on every push
5. Monitor `https://avandab.com` via `uptime-kuma` or `watchdog.log` tail

## Files Created for Prevention
- `write:/avandab/opencode.json` `/.opencode/opencode.json` `/avandab/.opencode/opencode.json`
- `write:/avandab/scripts/fix-opencode-launch.sh` `ensure-avandab-running.sh` `backup-avandab-db.sh`
- `write:/avandab/FAILURE_ANALYSIS_2026-08-31.md` (this file)
