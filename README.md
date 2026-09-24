# haproxy-lens

**English** | [Türkçe](README.tr.md)

A read-only agent that turns the HAProxy stats page into a readable dashboard, **without touching your HAProxy configuration**.

It is installed on each HAProxy server, reads that server's own stats data and logs, and shows the following in the browser. The dashboard and the installer messages are in Turkish.

- A plain-language status summary, e.g. "srv3 in api has been down for 12 minutes. Reason: connection timeout."
- Backends and servers: state, what the health check result means, connection usage, response time, errors.
- Live charts: requests per second (by response type) and traffic; the time range can be 5 min, 15 min, 1 hour, 6 hours or 24 hours.
- Which servers return the most 4xx and 5xx errors; if errors are spread evenly across servers, a hint that the problem is probably in something they share.
- A single summary at the top of the log section: a colored bar showing the 2xx/3xx/4xx/5xx split, what each class means, and plain sentences on where requests went (how many reached servers, how many HAProxy answered itself, how many reached no server at all).
- "Which address returns what" from the logs: tabs for 3xx, 4xx and 5xx showing which path returned which code (301, 404, 502...) how many times, including HAProxy's own http→https redirects.
- For failed and blocked requests, a detail view per row: the full address (if the host name is in the log), the real paths, and the client IPs.
- "Top client IPs": the addresses each IP requested most, so you can tell a normal user from a scanning bot.
- "Most requested addresses": click a row to see the 20 IPs that requested that address most, with the exact response codes each got (200, 404, 500...). This works regardless of the response code, so successful requests are covered too. Cloudflare IPs are labeled; direct clients are shown without a label.
- Open a backend to see which addresses and IPs sent requests to it: who is calling a backend that "should get no traffic", and where.
- From the logs: most requested addresses, blocked (403) requests and requests that matched no backend (503), top client IPs.
- A plain explanation for every term, and every field HAProxy reports for each row.

## Core principles

- **Never touches HAProxy's configuration or service.** No reload, no restart.
- **Adapts to each server.** By only reading the running HAProxy's configuration, the agent finds the stats socket, the log source and each frontend's log format by itself. Custom `log-format` definitions are read too.
- **Keeps history.** Charts and rates go back 24 hours by default. Data is written under `/var/lib/haproxy-lens` (a few hundred KB for 24 hours), so history survives an agent restart.
- **The server's own metrics.** CPU, I/O wait, memory, load average, disk usage and disk read/write speed, with live charts. Read from `/proc`: no extra privileges, tools or services needed.
- **Log search.** Searches the log files directly (including rotated and compressed ones), independently of the dashboard, so it can look further back than the retention period.
- **Follows changes while running, no reinstall needed.** When the configuration changes and HAProxy is reloaded (new log format, new Host capture, new backend), the agent notices within 30 seconds and updates itself. If the log source goes silent it looks for a new one; if the stats socket stops working it switches to another socket from the configuration.
- **Tells you what is missing.** If something in the configuration limits the data (logging off, `dontlog-normal`, host name not captured, no health checks, unreadable log lines...), the "Configuration notes" section at the top of the dashboard explains what it is, what it affects, and the configuration line you could add.
- **Does not install if unsure.** If it cannot find a working stats socket, it stops without changing anything and tells you why.
- **Only reads.** It sends HAProxy only the `show info` and `show stat` commands. There is no code that sends any other command (see `allowedCommands` in `haproxy.go`).
- **Keeps no sensitive data.** Query parameters in the logs (such as `?token=...`) are not kept, not even in memory.
- **Has a resource ceiling.** Runs at low priority with a 10% CPU limit and a 512 MB memory limit by default (set with `MEMMAX`); it cannot write under `/etc` or `/usr`. It uses about 50 MB in normal operation.
- **Saves on shutdown.** When the service is stopped or restarted (as during an update), it writes its history to disk before exiting; when it starts again it continues where it left off, never counts the same log line twice and does not skip lines written while it was down. Loading the history at startup can take a few seconds; meanwhile the dashboard is up and says the history is loading.
- **Browser protections on.** The dashboard cannot be embedded in another site (`frame-ancestors 'none'`), loads only its own files (Content-Security-Policy), and content types are not sniffed.
- **Not exposed to the internet.** The dashboard listens on the server's own internal IP (not on a keepalived VIP) and only answers requests from allowed networks. The default list is the private networks (10.x, 172.16-31.x, 192.168.x). If the server's main IP is public, the dashboard stays on `127.0.0.1`.
- **Proves it.** At the end of installation and removal it checks, and prints, that the sha256 of the configuration files and HAProxy's process IDs have not changed.

## Installation

All commands run on the HAProxy server as root.

### 1. Download and install

One line: downloads into a clean directory, verifies, extracts and installs. The steps are chained with `&&`, so if one fails the rest do not run.

```bash
cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh
```

wget prints one line per file, then the verification should say `haproxy-lens-linux-amd64.tar.gz: OK`. If the command ends without printing anything, the server cannot reach GitHub; see "If the server has no internet access" below. On ARM servers (if `uname -m` prints `aarch64`), use `arm64` instead of `amd64`.

The installer first shows a report, then what it is going to do, and asks for confirmation. At the end you should see these two lines (the installer's messages are in Turkish; they say the HAProxy configuration files did not change and HAProxy was neither restarted nor reloaded):

```
Doğrulama: HAProxy config dosyaları değişmedi (1 dosya, sha256 aynı).
Doğrulama: HAProxy yeniden başlatılmadı ve reload edilmedi (süreç numaraları aynı).
```

### 2. Only check, without installing

Replace `./install.sh` at the end of the line above with `./install.sh --check`. Nothing is installed; you only get the report:

```bash
./install.sh --check
```

The report has four parts: the configuration files and stats sockets found, the log source (and whether host names appear in the logs on that load balancer), configuration notes, and the address the dashboard will use. At the bottom there is a **SONUÇ** (result) line:

| Result | Meaning |
|---|---|
| `UYUMLU (stats + log analizi)` | Compatible: everything can be installed. |
| `UYUMLU (sadece stats)` | Compatible, stats only: the stats dashboard works fully, log analysis is off. The report says why. |
| `KURULAMAZ` | Cannot install: there is no working, accessible stats socket. Nothing is installed. |

### 3. Installation options

With no parameters, the installer uses these defaults:

| Parameter | Default | What it does |
|---|---|---|
| `PORT` | `8405` | Dashboard port |
| `LISTEN` | the server's internal IP | Dashboard address; if none is found, `127.0.0.1` (SSH tunnel only) |
| `ALLOW` | private networks (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `100.64.0.0/10`, `127.0.0.0/8`) | Networks allowed to reach the dashboard |
| `LOG` | `auto` | Log source; the agent finds it and follows it while running |
| `RETENTION` | `24h` | How long counts are kept |
| `DETAIL` | `1h` | How long full detail is kept (full address, real paths, IP breakdown) |
| `LISTS` | `6h` | How long path and IP lists are kept |
| `BUDGET` | `250` | Memory budget for detail (MB) |
| `MEMMAX` | `512M` | Memory ceiling of the service |

To change them, use one of these instead of `./install.sh` at the end of the line:

| Command | What it does |
|---|---|
| `./install.sh -y` | Installs without asking for confirmation |
| `PORT=8415 ./install.sh` | Uses a different port |
| `ALLOW=10.20.0.0/16 ./install.sh` | Only these networks can reach the dashboard; several networks or single IPs can be given, separated by commas |
| `LISTEN=10.0.0.5 ./install.sh` | Sets the dashboard address manually |
| `LISTEN=127.0.0.1 ./install.sh` | Makes the dashboard reachable only from the server itself (used with an SSH tunnel) |
| `LOG=/path/haproxy.log ./install.sh` | Pins the log source manually (default: the agent finds and follows it) |
| `RETENTION=48h ./install.sh` | How long counts are kept (default 24 hours, at least 1 hour) |
| `DETAIL=6h ./install.sh` | How long full detail is kept (full address, real paths, IP breakdown) (default 1 hour) |
| `LISTS=24h ./install.sh` | How long path and IP lists are kept (default 6 hours) |
| `BUDGET=500 ./install.sh` | Memory budget for detail, MB (default 250) |
| `MEMMAX=768M ./install.sh` | Memory ceiling of the service (default 512M) |

The last four decide how far back you can look and in how much detail; see [the section below](#how-far-back-and-in-how-much-detail).

On an update, every setting you do not give on the command line is kept from the previous installation (port, address, access list, log source, retention periods, memory budget and ceiling); the installer prints which ones it kept. Give only what you want to change: `DETAIL=12h ./install.sh` leaves the rest alone. If the previous dashboard address no longer exists on the server (the IP changed), the address is detected again. At the end, the installer prints the values it is running with.

### 4. Open the dashboard

Open the address printed at the end of the installation in your browser, for example `http://10.0.0.5:8405`.

If the dashboard was installed on `127.0.0.1`, run `ssh -L 8405:127.0.0.1:8405 root@SERVER_ADDRESS` on your own computer, then open `http://localhost:8405`.

If a firewall is active on the server (ufw, firewalld), you may need to allow the dashboard port for your network. The installer reminds you if it notices one, but does not touch the firewall.

### How the dashboard address is chosen

1. The interface carrying the default route is found (`/proc/net/route`).
2. keepalived VIPs on that interface are skipped (`/etc/keepalived/keepalived.conf` and the files it `include`s). Because a VIP moves between master and slave, the dashboard stays on each server's own address.
3. Of the remaining addresses, the one statically configured in the network settings is chosen: netplan, `/etc/network/interfaces`, `ifcfg-*`, NetworkManager, systemd-networkd.
4. If none is found, the interface's primary address is used; `/32`, `secondary` and labeled (`eth0:1`) addresses are skipped because they may be VIPs.
5. If the chosen address is a public IP, it is not used and the dashboard stays on `127.0.0.1`.

The "Panel adresi" (dashboard address) part of the `./install.sh --check` report shows which address was chosen and why, and which ones were skipped and why.

## Updating

Run the same one-line command as for installation: it always downloads the latest release, and the installer detects the previous installation and replaces it. All your settings are kept (port, address, access list, log source, retention periods, memory budget and ceiling); give only the ones you want to change.

## Removal

```bash
./uninstall.sh
```

Removes the service, the program file, the stored history and the `haproxy-lens` system user, and checks that nothing is left behind. It runs the same two verifications as the installer. If the package directory is gone, download the same package again and run the `uninstall.sh` inside it.

## If the server has no internet access

Download the package on your own computer, copy it to the server, then extract and install:

```bash
scp haproxy-lens-linux-amd64.tar.gz root@SERVER_ADDRESS:/root/
```

```bash
cd /root && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh
```

## Log search

The "Log'da ara" (log search) section at the bottom of the dashboard (the **Log'da ara** button at the top of the page takes you there) works independently of the dashboard data: it reads the log files directly, including rotated (`haproxy.log.1`) and compressed (`.gz`) files. So it can go much further back than the dashboard's retention period (24 hours by default).

You can search by: text contained in the address, IP (full or a prefix), HTTP method (GET, POST, DELETE…), status code (`500` or `5xx`) and time range. They can be used together or alone; for example you can pick only a method and search for "all DELETE requests in the last 24 hours". If a dashboard row's detail is no longer kept (the dashboard keeps detail only for the last hour), the "IP'leri ve zamanları log'dan getir" (get IPs and times from the log) button on that row searches for that request here, by its method and exact address. Results show the total number of matches, the status code breakdown, the top client IPs, the most matched addresses, and the newest matching requests with timestamps.

How it is protected:

- No shell commands are run; files are read inside the program, so search text cannot be used to run commands.
- Only the log source used by the agent and its rotated copies are read; no file path is accepted from the user.
- Lines that cannot match are dropped with a cheap text comparison before parsing. Parsing handles about 200 thousand lines per second, while this filter handles about 4 million, which makes searches on large logs several times faster. Results do not change: the filter only drops lines it can prove will not match, and the real filter still runs on the parsed record.
- Log files are read **from the end backwards**: the newest records are scanned first. So a "last 1 hour" search finishes quickly however large the file is, and even if it hits the time limit, the results cover the most recent records.
- A search runs for at most 1 minute, and only one search runs at a time (the agent's CPU limit is low). Searches for a specific code, IP or address finish in a few seconds; searches that match almost every line, such as only "GET" or "2xx", take longer. If the limit is reached, the result says so clearly.
- When several fields are filled in, they are combined with "and": requests matching all of them are found. A single field is enough too.
- When a time range is given, files last written before the range are not opened at all.

## Configuration notes

This section at the top of the dashboard lists everything in that server's configuration or environment that limits the dashboard. If there is a warning, it opens by itself. Each note says what is missing, what it affects, the line you could add to the configuration if you want, and where in the configuration it applies (such as `haproxy.cfg:39`). The same notes appear in the `./install.sh --check` report.

| Note | What it means |
|---|---|
| Frontend does not write logs | `no log`, or no log target; the log section cannot see this traffic |
| Traffic records are filtered by log level | Log targets are limited to a level such as `notice`; HAProxy writes traffic at `info` level |
| Not using an HTTP log format | No `option httplog`; path and status code are not in the logs |
| Missing fields in the log format | The custom `log-format` lacks the status code, path, backend/server or client IP |
| Successful requests are not logged | `option dontlog-normal` is on; only errors reach the logs |
| Host name not logged / captured only for some requests | The Host header is not captured, or captured conditionally |
| No health checks | Servers without `check`; if they go down HAProxy will not notice |
| Some log lines could not be read | The lines do not match the format in the configuration; examples are shown in the note (query parameters hidden) |
| Traffic in stats but not in the logs | Logging is off where this traffic passes, or the logs go somewhere else |

The agent writes nothing to the configuration. Adding a suggested line is your decision; if you add it, the dashboard adapts within 30 seconds after HAProxy is reloaded, and the note disappears.

## What the installation changes on the server

| What | Where |
|---|---|
| Program | `/usr/local/bin/haproxy-lens` |
| Stored history | `/var/lib/haproxy-lens` (created by systemd, removed by the uninstall script) |
| Service | `/etc/systemd/system/haproxy-lens.service` |
| System user | `haproxy-lens` (cannot log in) |

It writes to no other file. The service runs with the groups it needs to reach the stats socket and the logs: the socket's group (usually `haproxy`), `adm` for syslog files and `systemd-journal` for journald (if present on the server). So no reinstall is needed if the log location changes later. The user is not added to these groups permanently; the groups apply only while the service runs.

## What is supported

- **Stats socket:** unix path, `unix@`, `abns@`, `ipv4@` / `ipv6@` and `host:port` forms. If there are several sockets, the first working one is used.
- **Configuration:** all `-f` files and directories (such as `conf.d`) on the running HAProxy's command line.
- **Log source:** a syslog file (its location is found from the rsyslog/syslog-ng settings) or journald.
- **Log format:** `option httplog`, `option httpslog`, `option tcplog` and custom `log-format` definitions (including JSON-like formats). `defaults` inheritance, named `defaults` sections and `from` are supported. Unknown variables are skipped; if a field the dashboard needs is missing, it is reported as a note. `option httplog clf` (CLF) is not supported yet.
- **Operating system:** Linux distributions with systemd, amd64 and arm64.

## Host name (which domain the request was for)

haproxy-lens does not touch the configuration; it shows the host name as far as it can find it in the logs. On each load balancer it checks these sources by itself:

| The host name is in the logs if | Example |
|---|---|
| The Host header is captured | `capture request header Host len 64` or `http-request capture req.hdr(host) len 64` |
| The request is HTTP/2 | HAProxy writes `https://example.com/path` in the request line |
| `option httpslog` is used | From the SNI field at the end of the line |
| The host was appended to the `log-format` | `... %{+Q}r %[req.hdr(host)]` |

If none applies, the detail shows only the path and the IP. If the capture is conditional (e.g. `if rate_limit_abuse`), the host name appears only for those requests; the dashboard says for how many requests it is known. The `./install.sh --check` report also tells you whether host names are in the logs on that load balancer.

If you decide to add it on a load balancer where you want to see host names, the simplest way is the line `capture request header Host len 64` in the frontend. This line adds a `{example.com}` part to existing log lines; if another tool reads that log (such as Elasticsearch or fail2ban), check it first and try the change on a slave first.

## Known limitations

- **Host name:** The default `httplog` format does not include the Host; if no host name source is found on that load balancer (see the table above), requests returning 3xx, 4xx and 5xx show only the path and the IP.
- **Real client IP:** For requests coming through Cloudflare, the IP in the log belongs to Cloudflare; the dashboard labels these IPs "Cloudflare".
- **The log format is not guessed:** The agent reads lines according to the log definition in the configuration. Lines in a format not in the configuration (e.g. written to the same file by another server) cannot be read and appear in "Configuration notes" with examples.
- **History is kept in memory; the disk is only a backup.** So the real limit is memory, not disk; check the table below before extending the detail period.
- **Detail decreases over time:** Counts stay complete for the whole retention period, but address and IP detail covers the last hour and the lists the last 6 hours by default. The periods are adjustable; see [How far back, and in how much detail](#how-far-back-and-in-how-much-detail).
- **No permanent database:** History is kept in a single compressed file. For yearly trends or free-form queries, a system such as Prometheus is needed.
- **HAProxy restarts:** HAProxy's own cumulative counters (shown as "toplam", total) reset when HAProxy restarts. The charts and rates are kept separately and continue to include the earlier traffic.
- **Single server:** Each installation shows only its own server.
- **No password or HTTPS:** Access is limited only by network address. Anyone on an allowed network can see the dashboard; narrow it to your management network with `ALLOW` if needed.

## How far back, and in how much detail

Not all history is kept at the same level of detail: data is simplified step by step as it ages. The goal is to keep memory bounded; you decide how long each step lasts.

| Data age | What you see in the dashboard | Parameter (default) |
|---|---|---|
| 0 – 1 hour | **Everything.** Counts, path and IP lists, plus the detail shown when you click a row: full address (if the host name is in the log), real paths and client IPs | `DETAIL` (1 hour) |
| 1 – 6 hours | Counts, the busiest path and IP lists, plus the IP breakdown per address. No full address or real path detail | `LISTS` (6 hours) |
| 6 – 24 hours | **Counts only:** request count, 2xx/3xx/4xx/5xx split, per-backend breakdown, charts | `RETENTION` (24 hours) |
| Older than 24 hours | Deleted | |

The periods in the table are the defaults, used when you give no parameters.

Counts are never reduced at any step; only address and IP detail gets shorter. So charts and rates are complete for all 24 hours.

**To change it**, instead of `./install.sh` at the end of the installation command:

```bash
DETAIL=6h LISTS=24h BUDGET=500 MEMMAX=768M ./install.sh
```

In this example detail goes back 6 hours and lists 24 hours. See the table below for the memory cost; if you extend the detail period, raise the budget and the service ceiling too.

You can also make every step the same: `RETENTION=24h DETAIL=24h LISTS=24h BUDGET=750 MEMMAX=1G ./install.sh` keeps full detail for all 24 hours (~664 MB on a busy load balancer; if the budget is lower than that, the agent drops the oldest detail and the dashboard says so).

**The dashboard tells you what it has.** The log section shows the period the detail and lists *actually* cover, not the configured one. If the memory budget kicks in during a scanning attack and shortens the detail, you will see it there.

## Server metrics

The "Sunucu" (server) section of the dashboard shows the state of the machine HAProxy runs on: CPU usage, the time the CPU spends waiting for disk (I/O wait), memory, load average, disk usage and disk read/write speed. The cause of a slowdown is often here rather than in HAProxy.

The data is read from `/proc/stat`, `/proc/meminfo`, `/proc/diskstats` and file system information. These are readable by everyone, so no extra privileges are needed, and no shell commands are run. For disk usage, the root file system, the file system HAProxy logs to and the one where the agent keeps its history are watched (shown once if they are the same file system).

For disk I/O, only physical devices are counted (such as `sda`, `vda`, `nvme0n1`); partitions and mappings such as `dm-` and `loop` are skipped, otherwise the same reads would be counted twice.

History is kept the same way as for the HAProxy metrics: fine-grained for the last hour, per-minute averages beyond that, and the per-minute summary is written to disk so it survives an agent restart.

## Memory

History is kept in memory (the disk is only a backup for restarts), so the real limit is memory, not disk. With the default settings (counts for 24 hours, path/IP lists for 6 hours, full detail for 1 hour) it uses **~51 MB** on a busy load balancer.

The measurements can be reproduced with `go test -run TestBellekKullanimi`; the load used was ~44,000 requests per hour, 300 distinct addresses and 150 distinct IPs per minute.

| Setting | Memory |
|---|---|
| **Default:** detail 1 hour, lists 6 hours | ~51 MB |
| `DETAIL=6h LISTS=24h` | ~213 MB |
| `DETAIL=24h LISTS=24h` (full detail for everything) | ~664 MB |

Counts (requests, response codes, per-backend breakdown) stay complete for the whole retention period with every setting; the table only shows the cost of address and IP detail. On load balancers with less traffic these numbers are much lower.

**Memory budget.** Memory depends on the number of *distinct addresses* more than on the number of requests, and during a scanning attack every request can be a unique address. So besides the time limit there is a budget (250 MB by default): when it is exceeded, the agent drops the oldest detail by itself and the dashboard shows how many minutes the detail actually covers. In a test, a 6-hour attack with 8,000 unique addresses per minute was generated (2.88 million requests): memory stayed at 161 MB, detail was reduced to 50 minutes, and all counts were kept (`go test -run TestAtakDayanikliligi`).

**Service ceiling** (`MemoryMax`) is 512 MB. This is an upper limit, not a reservation; its purpose is to keep the service from being killed during an attack.

**Returning memory.** After a busy period, the agent does not only free memory internally but also returns it to the operating system. Go does not do this immediately by itself, and systemd applies the ceiling to RSS, so the return is triggered: right away after a large drop, and at most every 10 minutes for small drops. In a measurement, RSS went from 257 MB down to 100 MB within a few seconds after an attack.

Settings: `DETAIL=6h LISTS=24h BUDGET=500 MEMMAX=768M ./install.sh`. If you raise the budget, raise the service ceiling too.

## Troubleshooting

| Situation | What to do |
|---|---|
| The dashboard does not open | `systemctl status haproxy-lens` |
| The service runs but the dashboard has no data | `journalctl -u haproxy-lens -n 50` — the most common cause is the service user not being able to reach the stats socket |
| You forgot the dashboard address | `systemctl show haproxy-lens -p ExecStart` or `ss -ltnp \| grep haproxy-lens` |
| Which version is installed | `haproxy-lens -version` (also shown at the top of the dashboard, next to the name) |
| The browser says "Bu adresten panele erişim izni yok" (no access from this address) | Your network is not in the allowed list: reinstall with `ALLOW=<network>/<prefix> ./install.sh` |
| The page does not open at all, timeout | A firewall (ufw/firewalld) may be blocking the port; the installer warns if it notices, but does not touch it |
| The log section is empty or incomplete | "Configuration notes" at the top of the dashboard gives the reason and, if any, the configuration line you could add |
| You want to change settings | Running `ALLOW=... LISTEN=... ./install.sh` from the same package is enough; the service file is rewritten |
| The installer stopped with "HATA: ... olmalı" (error: must be ...) | One of the parameters you gave is invalid; the message says which one and gives an example. Nothing on the system was touched in this case |
| Search says "başka bir arama sürüyor" (another search is running) | The agent runs one search at a time (because of its low CPU limit); try again in a few seconds |

### Which values the installer accepts

Before touching the system, the installer checks the parameters and stops with a clear message if a value is invalid. The rules:

- Durations are written with a unit: `30m`, `6h`, `1h30m`. A bare `6` is not accepted.
- `RETENTION` is at least 1 hour and at most 30 days. `DETAIL` is at least 5 minutes. The order must be: `DETAIL` ≤ `LISTS` ≤ `RETENTION`.
- `BUDGET` is in megabytes, at least 16.
- `MEMMAX` is written with a unit (`512M`, `1G`) and must be at least 128 MB larger than `BUDGET`; otherwise the service would be killed before reaching the budget. (systemd treats a bare number as bytes; `MEMMAX=512` would kill the service as soon as it starts.)
- `PORT` is between 1 and 65535.

While running, the agent checks the configuration every 30 seconds. If you changed something in HAProxy and reloaded it, you do not need to do anything for the dashboard to update itself.

## How it works

```
HAProxy ──(stats socket: show info, show stat)──► haproxy-lens ──► dashboard (server's internal IP:8405)
   │                                                  ▲
   └──► syslog / journald ──(read only)───────────────┘
```

The agent reads the stats every 2 seconds and computes per-second values from the counter difference between two readings. It turns log lines into per-minute summaries and keeps them for the retention period (24 hours by default). The web interface is embedded in the program; no separate web server is needed.

## Development

Requirements: Go 1.22+ and Node 18+.

```bash
bash build.sh       # builds the interface and the program: ./haproxy-lens
go test ./...       # tests
```

Files:

| File | Contents |
|---|---|
| `main.go` | Parameters, web server, `/api/*` endpoints |
| `system.go` | Server metrics (`/proc/stat`, `/proc/meminfo`, `/proc/diskstats`, disk usage) |
| `store.go` | Writing the history to disk and loading it on restart |
| `haproxy.go` | Reading from the stats socket (the allowed commands are here), time range calculation |
| `config.go` | Reading haproxy.cfg: sections, `defaults` inheritance, log targets, Host capture |
| `logformat.go` | Turning a `log-format` definition into a parser (httplog, httpslog, tcplog, custom) |
| `logtail.go` | Following the log file / journald, per-minute summaries, path and code breakdowns |
| `watch.go` | Following the configuration, log source and socket while running; `/api/config` |
| `notes.go` | Configuration notes (what is missing, what it affects, which line could be added) |
| `detect.go` | Pre-installation detection and compatibility report (`-detect`) |
| `listen.go` | Choosing the IP the dashboard listens on (excluding keepalived VIPs) |
| `access.go` | Checking which networks may reach the dashboard |
| `cloudflare.go` | Built-in Cloudflare IP ranges (for labeling) |
| `search.go` | Log search: including rotated and compressed files, reading from the end backwards |
| `*_test.go` | Tests for the parser, configuration compatibility, memory, attack resilience, search, settings validation and access |
| `webapp/` | The dashboard interface (React) and icon (`favicon.svg`, `favicon.png`); `build.sh` builds it and embeds it in the program |
| `deploy/` | `install.sh` and `uninstall.sh` |

## Publishing a new release

1. Add the new version to `CHANGELOG.md`. Copy the "Kurulum ve güncelleme" (installation and update) part of the previous version as is; it is the same for every version.
2. On GitHub, go to **Releases > Draft a new release**, create a new tag (e.g. `v1.1.0`), and set the title to `haproxy-lens 1.1.0`.
3. Paste that version's whole section from `CHANGELOG.md` into the description (except the version number line at the top). This way the installation commands are ready on the release page.
4. Click **Publish release**. **Set as a pre-release** must not be checked, otherwise the `latest` address will not point to that version.
5. The `release` job in the **Actions** tab builds the packages and attaches them to the release within a few minutes. Wait for that job to turn green before running `wget` on the servers.

Version numbers: the last digit goes up for bug fixes (1.0.0 → 1.0.1), the middle one for new features (1.0.0 → 1.1.0).

## License and trademarks

MIT. See the `LICENSE` file for details.

This is an independent open-source project; it is not affiliated with, partnered with or endorsed by HAProxy Technologies. The name "HAProxy" is used only to identify the software it is compatible with. The project's icon is original and does not resemble the HAProxy logo.
