# SSH material for qemu+ssh

Place only the panel's dedicated SSH key, `config`, and `known_hosts` here.
The Docker Compose file mounts this directory at `/home/wvc/.ssh` read-only.
The container runs as uid/gid 10001, so fix ownership and modes after placing
the files:

```bash
sudo chown -R 10001:10001 deploy/ssh
sudo chmod 700 deploy/ssh
sudo find deploy/ssh -maxdepth 1 -type f \( -name 'id_*' -o -name config \) -exec chmod 600 {} +
sudo find deploy/ssh -maxdepth 1 -type f -name known_hosts -exec chmod 644 {} +
```

Do not commit private keys or copy the host's entire `~/.ssh` directory. The
service uses `BatchMode=yes` and `StrictHostKeyChecking=yes`, so host keys must
already be present in `known_hosts`.
