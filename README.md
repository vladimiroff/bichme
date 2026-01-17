# bichme

bichme [(`/biːtʃˈmɛ/`)](https://vld.bg/bichme/bichme.mp3) is utility for quick
and dirty command execution on multiple machines at once.

Yes, you have Ansible for configuration management. Loki and Kibana for logs.
Grafana for metrics. Prometheus alerts for when things go sideways. A perfectly
crafted CI/CD pipeline. Infrastructure as code. GitOps. The works.

And yet here you are, mass-grepping 947 servers for that one config line you're
not sure actually got deployed. Sometimes you just need to run `uptime` or
check free space with `df -h /` on everything and call it monitoring. Or maybe
you want to do a mass-upgrade for the latest CVE with a domain and logo.

bichme connects to multiple servers via SSH in parallel, executes commands or
scripts, and aggregates the output. No YAML. No inventory files. No plugins.
Just a list of hosts and a command.

## Installation

```sh
go install vld.bg/bichme/cmd/bichme@latest
```


## Quick Start

Create a file with your target hosts (one per line):

```
# servers.txt
web01.example.com
web02.example.com
db01.example.com:2222  # custom port
```

Run a command on all of them:

```sh
bichme shell servers.txt uptime
```

Or upload and execute a script:

```sh
bichme exec servers.txt ./deploy.sh
```

## Commands

### shell

Run a shell command on multiple machines.

```sh
bichme shell <servers-file> <command> [flags]
```

Example:

```sh
bichme shell servers.txt df -h /
bichme shell servers.txt 'systemctl status nginx | head -5'
```

### exec

Upload and execute a file on multiple machines. The file is transferred via SFTP and then executed.

```sh
bichme exec <servers-file> <file> [flags]
```

Example:

```sh
bichme exec servers.txt ./scripts/health-check.sh
bichme exec servers.txt ./deploy.sh -f config.yaml -f secrets.env
```

The `-f` flag uploads additional files alongside the main executable. Useful for configs, data files, or dependencies your script needs.

### upload

Upload files to multiple machines via SFTP.

```sh
bichme upload <servers-file> <pattern>... [flags]
```

Example:

```sh
bichme upload servers.txt migrations/*.sql
bichme upload servers.txt package.tar.gz -o ~/deploy
```

### download

Download files from multiple machines via SFTP. Files are stored in per-host subdirectories.

```sh
bichme download <servers-file> <pattern>... [flags]
```

Example:

```sh
bichme download servers.txt /var/log/*.log -o ~/logs
bichme download servers.txt '/etc/nginx/*.conf'
```

### history

View and manage execution history.

```sh
bichme history           # list recent executions
bichme history show 1    # show details of execution #1
bichme history purge --keep 10        # keep only last 10
bichme history purge --older-than 24h # delete older than 24h
bichme history purge --all            # delete everything
```

## Flags

Common flags for `shell` and `exec`:

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--user` | `-u` | current user | SSH user to connect as |
| `--port` | `-p` | 22 | SSH port |
| `--workers` | `-w` | 10 | Number of parallel connections |
| `--retries` | `-r` | 5 | Retry count for failed operations |
| `--conn-timeout` | | 30s | Connection timeout |
| `--exec-timeout` | `-t` | 1h | Execution timeout |
| `--history` | | true | Record execution in history |
| `--insecure` | `-i` | false | Skip host key verification |

Global flags:

| Flag             | Short | Default                          | Description                  |
| ---------------- | ----- | -------------------------------- | ---------------------------- |
| `--verbose`      | `-v`  | false                            | Enable debug output          |
| `--history-path` |       | `~/.local/state/bichme/history/` | Where to store history       |
| `--upload-path`  |       | `~`                              | Remote directory for uploads |

## Hosts File Format

One host per line. Empty lines and comments (starting with `#`) are ignored.

```
# Production web servers
web01.example.com
web02.example.com

# Database servers (custom port)
db01.example.com:5022
db02.example.com:5022

# Can also specify user inline
admin@legacy.example.com
```

Duplicate hosts are automatically removed.

## SSH Authentication

bichme uses your SSH agent for authentication and whatever unencrypted keys it
could find by reading identity files from `~/.ssh/` (id_rsa, id_ed25519, etc.).

```sh
eval $(ssh-agent)
ssh-add
```

Host keys are verified against `~/.ssh/known_hosts` and `/etc/ssh/ssh_known_hosts`. Use the `--insecure` flag to skip verification (not recommended).

## Runtime Signals

Send `SIGUSR1` to a running bichme process to print current execution statistics:

```sh
kill -USR1 $(pgrep bichme)
```

## Output

Each line of output is prefixed with the hostname:

```
web01:  15:42:01 up 42 days,  3:21,  0 users,  load average: 0.08, 0.03, 0.01
web02:  15:42:01 up 38 days,  1:15,  0 users,  load average: 0.12, 0.08, 0.02
db01:   15:42:01 up 99 days,  8:44,  0 users,  load average: 0.45, 0.32, 0.28
```

At the end of execution, a summary is printed:

```
============== 6 ==============
 Connection failed:     1
 Execution failed:      3
 Done:                  38
===============================
 Total: 42
===============================
```

## Examples

### Basic Operations

```sh
# Check uptime across all servers
bichme shell servers.txt uptime

# Check disk usage on root partition
bichme shell servers.txt 'df -h /'

# Get memory info from all hosts
bichme shell servers.txt 'free -m'
```

### Commands with Pipes and Quotes

When your command includes pipes, redirects, or special characters, wrap it in quotes:

```sh
# Check nginx status (first 5 lines only)
bichme shell servers.txt 'systemctl status nginx | head -5'

# Find large log files
bichme shell servers.txt 'find /var/log -size +100M -exec ls -lh {} \;'

# Count active connections
bichme shell servers.txt 'ss -tuln | grep LISTEN | wc -l'
```

### Controlling Parallelism

```sh
# Rolling restart: one server at a time
bichme shell servers.txt 'systemctl restart myapp' -w 1

# Aggressive parallelism for quick checks
bichme shell servers.txt hostname -w 5000
```

### Different Users and Ports

```sh
# Combine as root on non-standard SSH port
bichme shell servers.txt 'whoami' -u admin -p 2222
```

### Timeouts and Retries

```sh
# Quick timeout for health checks
bichme shell servers.txt 'curl -s localhost:8080/health' -t 10s

# Long-running database backup
bichme exec servers.txt ./backup.sh -t 4h

# Unreliable network? More retries
bichme shell servers.txt uptime -r 10 --conn-timeout 60s
```

### Uploading and Executing Scripts

```sh
# Simple script execution
bichme exec servers.txt ./scripts/health-check.sh

# Deploy script with config file
bichme exec servers.txt ./deploy.sh -f config.yaml

# Multiple support files
bichme exec servers.txt ./install.sh -f package.tar.gz -f settings.json
```

### Filtering and Processing Output

bichme output can be piped through standard Unix tools:

```sh
# Sort servers by load average
bichme shell servers.txt uptime 2>/dev/null| sort -t: -k2 -rn

# Find servers with high disk usage
bichme shell servers.txt 'df -h /' | grep -E '[89][0-9]%|100%'

# Only show failures (non-zero exit)
bichme shell servers.txt 'systemctl is-active myapp' 2>&1 | grep -v '^.*: active'

# Save output for later analysis
bichme shell servers.txt 'cat /etc/os-release' > os-versions.txt
```

### Debugging and Troubleshooting

```sh
# Verbose output shows connection details
bichme shell servers.txt uptime --verbose

# Check progress on long-running job (in another terminal)
kill -USR1 $(pgrep bichme)

# Review what you ran yesterday
bichme history
bichme history show 42
```

### Skipping History

```sh
# Don't record sensitive operations
bichme shell servers.txt 'echo $SECRET_KEY' --history=false
```

## License

MIT License. See [LICENSE](LICENSE) for details.
