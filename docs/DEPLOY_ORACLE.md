# Deploy on Oracle Cloud Always Free

Why: free VM with a real disk, so embedded SQLite works as is. Cost $0. Oracle asks for a card to check identity; Always Free resources are not billed.

Facts to re-check on Oracle's pages before relying on them (they change): the Always Free ARM allowance (reported cut to 2 OCPU / 12 GB), the idle-reclaim rule (CPU, network and memory under 20% for 7 days), and the Object Storage free size.

## Part A: what you do in the browser (about 20 minutes, once)

1. Create the account at cloud.oracle.com (Always Free). Pick the home region closest to your users (Canada Southeast, Montreal, if offered). The region cannot be changed later.
2. Console, Compute, Instances, Create instance:
   - Image: Ubuntu 24.04 (or newest LTS).
   - Shape: Ampere A1 (VM.Standard.A1.Flex), 2 OCPU, 12 GB. If "out of capacity", retry later or try another availability domain. Fallback: the free AMD micro (VM.Standard.E2.1.Micro), same scripts with `amd64`, but only 1 GB RAM.
   - Add your SSH public key (paste it, or let Oracle generate and download the private key).
   - Boot volume: default (up to 200 GB total is free).
3. Networking: Virtual Cloud Network, your subnet, Security List: add ingress rules for TCP 80 and 443 from 0.0.0.0/0. (Port 22 is already open.)
4. Free domain with HTTPS (passkeys need a real domain): at duckdns.org sign in, create a name such as `numerix-api.duckdns.org`, set its IP to the instance's public IP.
5. Object Storage, create a bucket `numerix-backup` (private). Profile, Customer Secret Keys, generate a key pair: you get an access key id and a secret. Note your tenancy namespace and region (for example `ca-montreal-1`).
6. Send me: the VM public IP, the SSH user (`ubuntu`), the domain, the namespace and region. Never paste the secret key in chat; put it on the VM yourself (step B4).

## Part B: what the scripts do (I run them with you, or you run them)

1. Copy `deploy/oracle/` to the VM: `scp -r deploy/oracle ubuntu@<ip>:~/` then on the VM `sudo bash ~/oracle/setup.sh numerix-api.duckdns.org`.
2. On the VM edit `/etc/numerix/api.env`: `JWT_SECRET` and `ADMIN_API_TOKEN` (`openssl rand -hex 32` each), `WEBAUTHN_RP_ID` (the domain), origins. Edit `/etc/litestream.yml` placeholders (namespace, region).
3. From your PC: `bash deploy/oracle/deploy.sh ubuntu@<ip> arm64`.
4. Backup credentials, on the VM: `sudo systemctl edit litestream` and add
   `[Service]` / `Environment=LITESTREAM_ACCESS_KEY_ID=...` / `Environment=LITESTREAM_SECRET_ACCESS_KEY=...`, then `sudo systemctl restart litestream`.
5. Check: `curl https://numerix-api.duckdns.org/v1/health` returns `{"status":"ok"...}`.
6. Restore drill (do it once, before trusting backups): on a scratch folder `litestream restore -config /etc/litestream.yml -o /tmp/restored.db /var/lib/numerix/numerix.db` and open the file with sqlite.
7. Point the web app at it: on Vercel set `API_URL=https://numerix-api.duckdns.org/v1`, `PLATFORM_API_URL` for the admin app, redeploy.

## Keeping the VM from being reclaimed

The keep-alive in the web repo pings health every 5 minutes. Oracle reclaims on low CPU, network and memory use together for 7 days, so also keep a light scheduled job (the daily backup verification) running. Watch Oracle emails.

## Updating

`bash deploy/oracle/deploy.sh ubuntu@<ip> arm64` rebuilds, copies and restarts (about 10 seconds of downtime; SQLite migrations run on start).

## Rollback

Keep the previous binary: `ssh ... 'sudo cp /opt/numerix/api /opt/numerix/api.prev'` before deploying; to roll back copy it back and `systemctl restart numerix-api`. Database restore: stop the service, `litestream restore`, start.
