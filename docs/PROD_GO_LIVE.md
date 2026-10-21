# Numerix production go-live package

Written 2026-10-21 for the owner. Plain English on purpose. Every fact about a price, a limit or a rule carries a source and the date it was checked, or the word UNVERIFIED with a way to verify it. Nothing in this file promises that go-live will be perfect. What it gives you is a set of checks (gates) that make a failure visible early and reversible, listed in section 21.

Where this file lives: `Numerix/docs/PROD_GO_LIVE.md`, copied into `Services/numerix-api/docs/` and `Services/frontend/docs/`. The one-page tick list is `PROD_OWNER_CHECKLIST.md`.

Words used: **VM** = a rented computer in a data centre. **Staging** = a full copy of the product used only to rehearse; no real families. **Production** = the real thing. **Release** = one numbered build of the software. **Smoke test** = a short script that checks the main things work.

---

## 1. The big picture

```
                         INTERNET (families, parents, you)
                                    |
                 DNS (your registrar):  app.  api.  admin.   (+ staging-app. staging-api. staging-admin.)
                                    |
              +---------------------+-----------------------+
              |  PRODUCTION VM                              |  STAGING VM (same layout, own secrets, own data)
              |  Caddy :80/:443  (HTTPS certificates, auto) |  Caddy :80/:443
              |    app.   -> 127.0.0.1:3000  Next.js web    |    staging-app.   -> :3000
              |    admin. -> 127.0.0.1:3100  Next.js admin  |    staging-admin. -> :3100
              |    api.   -> 127.0.0.1:8081  Go API         |    staging-api.   -> :8081
              |  Go API  --> SQLite file /var/lib/numerix/numerix.db
              |  Litestream --> continuous copy to object storage (Oracle bucket, plus Backblaze B2 copy)
              |  numerix-deploy gate  <--- SSH, restricted deploy key (GitHub Actions only)
              +---------------------------------------------+

   Browser -> web app (same address, cookies) -> web server calls the API on 127.0.0.1 (server to server, bearer token)
   Browser -> admin app -> admin server calls the API on 127.0.0.1 with the admin token (the browser never sees it)

   GitHub (2 repos)                                  Optional, off by default
     numerix-api  : CI -> deploy-staging (on push to main) -> deploy-prod (tag vX.Y.Z + HUMAN APPROVAL)
     numerix-web  : CI -> deploy-staging (on push to main) -> deploy-prod (tag vX.Y.Z + HUMAN APPROVAL)
                                                      MongoDB Atlas M0: copy of bests / family board / anonymous stats
```

Key facts verified in the code (2026-10-21, branch `release/r3`):

- The browser talks only to the web app. The web app calls the API from the server (`src/app/api/v1/[...path]/route.ts`). So the API does not need CORS for the web app, and all cookies belong to the web app address. The API is still reachable at `api.` for health checks, the admin proxy and tools.
- The API listens on one port and stores everything in one SQLite file. One writer. That is why there is one API process per environment and no load balancer.
- No email is sent by any code in either repo (searched for SMTP and mail libraries: none; the API has no email verification or password reset endpoints yet, they are listed as BE-34/BE-35 in `contracts/BACKEND_INTEGRATION.md`). See section 10.

---

## 2. Environments

Staging and production both run the API with `APP_ENV=production`. That is deliberate: the strict checks (long secrets, https-only origins, Secure cookies, JSON logs, no profiler) are what you want to rehearse. The API accepts only `development`, `test`, `production`.

| | Local (developer PC) | Staging | Production |
|---|---|---|---|
| Purpose | Building and tests | Rehearse every release, real browsers, fake families | Real families |
| Web app | http://localhost:3000 | https://staging-app.YOURDOMAIN | https://app.YOURDOMAIN |
| Admin app | http://localhost:3100 | https://staging-admin.YOURDOMAIN | https://admin.YOURDOMAIN |
| API | http://localhost:8081 | https://staging-api.YOURDOMAIN | https://api.YOURDOMAIN |
| Passkey RP id | `localhost` | `staging-app.YOURDOMAIN` | `app.YOURDOMAIN` |
| Database | `data/*.db` on the PC | SQLite on the staging VM | SQLite on the production VM |
| Backups | none | Litestream to bucket path `staging/` | Litestream to bucket path `prod/`, plus B2 copy |
| Secrets | dev defaults | their own random values | their own random values (never reused from staging) |
| Who deploys | the developer | GitHub Actions on every push to `main`, no approval | GitHub Actions on a version tag, only after YOU press Approve |
| `APP_ENV` | development | production | production |

Rule: staging and production never share a secret, a database, a bucket path or a deploy key.

---

## 3. Where the web and admin apps run (the hosting decision)

### 3.1 The Vercel fact you must know

VERIFIED 2026-10-21 against Vercel's pages (https://vercel.com/docs/plans/hobby and https://vercel.com/docs/limits/fair-use-guidelines): "Hobby teams are restricted to non-commercial personal use only. All commercial usage of the platform requires either a Pro or Enterprise plan." Commercial usage is defined as any deployment used for the financial gain of anyone involved in any part of the production of the project, including "any method of requesting or processing payment from visitors" and "advertising the sale of a product or service". Donations alone are not commercial.

Numerix has paid plans (billing and a subscription gate are built in). Production on a Vercel Hobby account is therefore NOT allowed. The current Vercel setup (`numerix-web-snowy.vercel.app`) is fine for development demos only. Vercel Pro costs $20 per developer seat per month plus usage (same Hobby page, checked the same day). Deleting the Vercel account is fine: nothing in the code needs it. Two Vercel-only helpers (`vercel.ts` headers, `scripts/vercel-env.mjs`) become unused; the self-host build carries the same security headers (section 15).

### 3.2 Options compared

| Option | Commercial use OK? | Extra cost | Honest trade-offs |
|---|---|---|---|
| **A. Self-host web + admin on the same VM as the API, behind Caddy (RECOMMENDED)** | Yes (your own server) | $0 on top of the VM | You patch the OS and watch the server. Web, admin and API share one machine: if it dies, all three die (backups and rollback cover data, not downtime). Data stays in one region next to the API. One less vendor. Built and tested in this package. |
| B. Vercel Pro | Yes | $20 per seat per month (VERIFIED), plus usage | Least work to run. Servers outside your control. The API still needs its own host. The cheapest paid path for the front end, but it does not reduce what you pay for the API host. |
| C. Cloudflare (Workers) | Likely yes (UNVERIFIED terms) | Free plan: 100,000 requests/day and 10 ms CPU per request; paid plan minimum $5/month (VERIFIED https://developers.cloudflare.com/workers/platform/pricing/) | Next.js 16 on Workers needs an adapter. NOT tested with this repo (video Range requests, size limits, rewrites). UNVERIFIED. Verify by building a staging copy before trusting it. |
| D. Netlify | The pricing page does not say whether commercial use is allowed on Free (UNVERIFIED, read their terms). Paid Pro $20/month, Personal $9/month (VERIFIED https://www.netlify.com/pricing/ 2026-10-21) | $9 to $20 | Same trade-offs as B. Not tested with this repo. |

**Decision: A.** Reasons: it is allowed for commercial use, it costs nothing extra, the web server talks to the API on the same machine (fast, and the API does not even need to be exposed to the web app), and everything in this package is built and tested for it. The paid path if you later want less to operate is B.

What option A gives up: no automatic scaling, no global CDN (images and videos are cached by the browser for a year because file names carry their content hash; a CDN can be put in front later), and you are the system administrator. Server patches: the setup script turns on automatic security updates.

---

## 4. Costs (all prices checked 2026-10-21; USD unless stated; CAD figures are NOT converted here, check the day's rate)

VERIFIED = read on the vendor's own page on that date. SECONDARY = a comparison site or forum; treat as a hint. UNVERIFIED = not found.

| Item | Option | Monthly | Yearly | Source and status |
|---|---|---|---|---|
| App + admin hosting | Self-host on the API VM | $0 extra | $0 | this package |
| | Vercel Pro | $20 per seat | $240 | VERIFIED https://vercel.com/docs/plans/hobby |
| | Cloudflare Workers paid | from $5 | from $60 | VERIFIED https://developers.cloudflare.com/workers/platform/pricing/ (compatibility UNVERIFIED) |
| | Netlify Pro / Personal | $20 / $9 | $240 / $108 | VERIFIED https://www.netlify.com/pricing/ (commercial terms UNVERIFIED) |
| API host | Oracle Always Free (Montreal home region possible) | $0 | $0 | VERIFIED https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm |
| | OVHcloud VPS-1, 2 vCores, 4 GB, 40 GB NVMe, Beauharnois, Quebec | from $6.20 | about $74.40 | Price and plan: VERIFIED https://www.ovhcloud.com/en-ca/vps/ . Location "Beauharnois (Quebec)": VERIFIED https://www.ovhcloud.com/fr-ca/vps/vps-canada/ . Currency (CAD or USD) is not stated on the page: UNVERIFIED. The configure link says `upfront12`, which suggests the price assumes paying 12 months up front: UNVERIFIED, read the order screen. |
| | Hetzner CAX11 (ARM, 2 vCPU, 4 GB) | EUR 5.99 (from 15 June 2026) | EUR 71.88 | SECONDARY https://privatedevops.com/news/hetzner-june-2026-cloud-price-increase-what-to-do . Locations found: Germany, Finland (US sites exist). No Canadian site found: data would leave Canada. |
| | Vultr / DigitalOcean entry plans | about $5 to $6 | about $60 to $72 | SECONDARY only. Toronto availability and price UNVERIFIED. Contabo: not researched. |
| Backup storage | Oracle Object Storage | $0 up to 20 GB | $0 | VERIFIED Oracle page above (20 GB and 50,000 requests/month in the "Always Free only" state) |
| | Backblaze B2 | $0 for the first 10 GB, then $6.95 per TB | about $0 for this database | VERIFIED https://www.backblaze.com/cloud-storage/pricing (free egress up to 3x stored data) |
| | Cloudflare R2 | $0 for the first 10 GB, then $0.015 per GB-month, no egress fee | about $0 | VERIFIED https://developers.cloudflare.com/r2/pricing/ |
| Domain | Porkbun `.ca` | about $0.77 | register $8.77 on sale (regular $11.12), renew $9.18 on sale (regular $11.52) | VERIFIED https://porkbun.com/products/domains . WHOIS privacy free: VERIFIED same page. The sale may end: read the cart. |
| | Porkbun `.com` | about $0.92 | $11.08 register and renew | VERIFIED same page |
| | Namecheap `.ca` renewal | about $1.00 | $11.98 | SECONDARY (domainoffer.net, Aug 2026). WHOIS privacy free there: SECONDARY. |
| | Cloudflare Registrar | at cost, no markup | UNVERIFIED for `.ca` (a forum says `.ca` was added; not on an official page I could open) | https://blog.cloudflare.com/registrar-for-everyone/ . Check the registrar's `.ca` list before counting on it. |
| Monitoring | GitHub Actions scheduled check (already in the repo: `keepalive.yml`) | $0 | $0 | free for what we use; 5-minute schedules may be delayed by GitHub: UNVERIFIED limit |
| | UptimeRobot free | $0 | $0 | CONFLICTING: several comparison sites say the free plan is personal and non-commercial only since late 2024, but UptimeRobot's own page says it is available for any use. UNVERIFIED: read their terms before using it for a paid product. |
| | Better Stack / Healthchecks.io free tiers | $0 | $0 | UNVERIFIED limits and terms; check before use |
| Email | none needed today | $0 | $0 | no code sends email (section 10). When BE-34/BE-35 are built you will need a sender (a free-tier transactional service or your registrar's mailbox): price UNVERIFIED until you pick one |
| Legal | privacy policy and terms review by a Quebec lawyer | one-off | one-off | cost UNVERIFIED, ask for a quote |

### 4.1 Totals

**(a) Cheapest total that is still safe for production with children's data**

- Production on a paid VPS in Quebec: OVHcloud VPS-1 at $6.20/month.
- Staging on the Oracle Always Free VM ($0). If losing staging someday is acceptable it is fine, because staging holds only fake data.
- Backups: Litestream to an Oracle bucket AND to a Backblaze B2 bucket (free under 10 GB).
- Domain: Porkbun `.ca` about $9.18/year.
- Monitoring: GitHub scheduled check, plus a free uptime service after you read its terms.
- Total: about **$7 per month, about $84 per year** (USD, before tax). If OVH bills $6.20 in CAD it is lower. Open items: OVH currency and commitment.

**(b) Free-tier-only total**

- Production and staging on two Oracle Always Free VMs, backups to the Oracle bucket plus the Backblaze free 10 GB, free monitoring, free software.
- The only unavoidable cost is the domain: **about $0.77/month ($9.18/year)**. A free name from DuckDNS would make it $0 but is a poor fit for a paid children's product (shared name, no brand, terms UNVERIFIED) and I do not recommend it.
- What it risks: see 5.3. Oracle can reclaim an "idle" machine and can refuse a sign-up. Because a small product stays under Oracle's 20 percent thresholds, it WILL look idle.

**(c) Recommended: (a)**, with a clear reason: the only part of the free path that can destroy a family's data or take the site down without warning is the Oracle machine for production, and it costs about $6 a month to remove that risk. Everything else free is safe because it is either rehearsal (staging) or a copy (backups).

What (a) sacrifices, honestly:

- One server, so no high availability: a server failure means downtime until you restore (rollback and restore are rehearsed, section 12).
- A 12-month commitment may apply (UNVERIFIED).
- Staging on Oracle is ARM and production at OVH is x86, so staging does not exercise the exact production binary. The Go program is a static build for both, so the risk is small, but it is a real difference. If you want identical machines, rent a second OVH VPS-1 for staging (about $12.40/month total).
- A fallback if your Oracle sign-up is refused: the free tier cannot be created, so staging then needs a second paid VPS.

Never chosen because they risk data or break terms: production on Vercel Hobby (not allowed), production data only on a free VM with no off-site copy, the MongoDB free cluster as the only store (no backups), `0.0.0.0/0` database access.

---

## 5. Hosting the API: Oracle Always Free, or a paid VPS

### 5.1 Oracle facts (VERIFIED 2026-10-21, https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm)

- Ampere A1 shape (`VM.Standard.A1.Flex`): "the first 1,500 OCPU hours and 9,000 GB hours per month ... For Always Free tenancies, this is equivalent to 2 OCPUs and 12 GB of memory." This is lower than the 4 OCPU / 24 GB you may have read about elsewhere; plan on **2 OCPU / 12 GB in total**, which matches the note already in `DEPLOY_ORACLE.md`.
- Storage: 200 GB total block and boot volume combined; each instance gets a boot volume of about 47-50 GB.
- Object Storage: 20 GB, 50,000 requests a month in the Always Free state. Outbound data: 10 TB a month.
- Always Free compute must be created in your **home region**, chosen at sign-up and not changeable. Pick Canada Southeast (Montreal) if offered, so the data stays in Quebec.
- Reclaim rule, exact wording: Oracle "may" reclaim an instance as idle if in a 7-day period CPU use at the 95th percentile is under 20 percent, network use is under 20 percent, and (for A1 shapes only) memory use is under 20 percent. A small product easily stays under all three.
- I found no account-inactivity rule on that page. Sign-up rejection (card or identity check failing) is widely reported by users but is NOT documented on that page: UNVERIFIED.

### 5.2 Recommended layout on Oracle (free path)

Two separate VMs, not one VM with two copies: `numerix-prod` (1 OCPU, 8 GB) and `numerix-staging` (1 OCPU, 4 GB) in the same tenancy. This totals 2 OCPU / 12 GB, the whole allowance. Reasons: a bad staging deploy cannot starve production, and each has its own deploy key, secrets and database.

The risk to state plainly: both machines live in ONE Oracle account. If that account is closed or reclaimed, you lose both, and your off-site backup (Backblaze) is what saves you. This is why the production database is replicated to two places.

One VM with two copies of the software is NOT supported by the scripts in this package (ports and file paths are fixed). If you want that, it is a code change.

### 5.3 Why production should not sit only on a free VM

Oracle can reclaim it, Oracle can also stop an Always Free account that fails a check, and free machines have no support. A reclaim deletes the boot volume. For a product holding children's data and paying customers this is the one risk worth about $6 a month. Everything else about Oracle (staging, the bucket) is fine.

### 5.4 Data residency (Quebec)

Oracle Montreal and OVH Beauharnois are both in Quebec. Hetzner (Germany, Finland, USA) and most US-named clouds are not. Law 25 does not forbid hosting outside Quebec but requires a privacy impact assessment before personal information leaves Quebec and that it gets equivalent protection (see section 13). Staying in Quebec removes that question. Backblaze B2 is a US company; its data centre regions (including a Canadian one) are UNVERIFIED here. The backup holds children's information, so before the B2 copy goes live: pick its region deliberately, and add it to your privacy assessment. Backups can be encrypted before upload (not done by this package; see UNVERIFIED list).

---

## 6. Domain, DNS and HTTPS

1. Buy the domain. Recommended: Porkbun, `.ca` (VERIFIED prices in section 4; free WHOIS privacy). `.ca` requires Canadian presence rules (CIRA): you are in Quebec so you qualify; the exact rules are UNVERIFIED here, the registrar will show them at checkout.
2. At the registrar, DNS page of the domain, add these records (replace the two IPs with the VM public IPs; TTL 300 seconds is fine):

| Type | Host (name) | Value | Used for |
|---|---|---|---|
| A | `app` | PRODUCTION VM public IP | web app |
| A | `admin` | PRODUCTION VM public IP | admin app |
| A | `api` | PRODUCTION VM public IP | API |
| A | `staging-app` | STAGING VM public IP | staging web |
| A | `staging-admin` | STAGING VM public IP | staging admin |
| A | `staging-api` | STAGING VM public IP | staging API |

No AAAA records unless you gave the VM an IPv6 address and tested it. No CAA record (Caddy may get certificates from Let's Encrypt or ZeroSSL, a CAA record that names only one would break renewal; UNVERIFIED which CA Caddy picks).
3. HTTPS: Caddy requests and renews a free certificate for each host by itself, as soon as the DNS names point at the VM and ports 80 and 443 are open. You do nothing. The smoke tests check that the certificate has at least 14 days left.
4. Cloud firewall (Oracle: Networking, Virtual Cloud Networks, your network, Security Lists, Add Ingress Rules): allow TCP 22 (your own IP only if you can), 80 and 443 from `0.0.0.0/0`. Nothing else. The API, web and admin servers listen only on `127.0.0.1`, so they are not reachable from outside even if a rule were wrong (`LISTEN_HOST=127.0.0.1` for the API; `HOSTNAME=127.0.0.1` in the web and admin service files).
5. Host firewall: Oracle's Ubuntu image blocks 80 and 443 in `iptables` even after the cloud rule opens them. `setup.sh` fixes that.
6. Add an `app.` check that port 3000 is not reachable: from your PC `curl -m 5 http://PRODUCTION_IP:3000` must fail.

---

## 7. What gets installed on a VM (and where)

```
/opt/numerix/api|web|admin|seed/releases/<release id>/   one folder per release (last 5 kept)
/opt/numerix/<component>/current   -> link to the active release (rollback moves this link)
/opt/numerix/<component>/previous  -> link to the one before
/opt/numerix/node/                 Node.js 22 (pinned, checksum verified)
/etc/numerix/api.env  web.env  admin.env     the settings and secrets (root and group numerix only)
/etc/litestream.yml  /etc/litestream.env     backup target and its access keys
/var/lib/numerix/numerix.db        the database;  /var/lib/numerix/backups/  pre-deploy copies (last 14)
/var/lib/numerix/feed-media/       the 200 Discover files (copied by hand once per release)
/usr/local/bin/numerix-deploy      the only command the CI deploy key may run
systemd units: numerix-api, numerix-web, numerix-admin, caddy, litestream, numerix-verify-backup.timer
```

Users: `numerix` runs the services and cannot log in. `deploy` has a key that works only for `numerix-deploy` (forced command, no shell, no port forwarding). Sudo for `deploy` is limited to restarting the three services and running the backup, restore and seed helpers as `numerix`.

Verbs of the gate, tested offline in WSL (Ubuntu) on 2026-10-21 with `deploy/oracle/test-gate.sh`: put (with checksum, refuses overwrites, unsafe archives, bad names), activate, rollback, verify, backup, restore-db, seed, status.

---

## 8. Secrets inventory

Rules: generate on your own PC or on the VM; paste only into the place named; never into chat, email, a ticket or the repo. A scanner (`scripts/secret-scan.sh`, run by CI) looks for common secret shapes in files and history.

Generate a strong value: `openssl rand -hex 32` (Mac, Linux, Git Bash on Windows).

### 8.1 API (`/etc/numerix/api.env` on each VM)

| Name | Purpose | Where set | How generated | Rotation |
|---|---|---|---|---|
| `JWT_SECRET` | signs login tokens (32+ chars enforced) | VM file | `openssl rand -hex 32`, different on staging and prod | Change it, restart: everyone signs in again. Do it if you suspect a leak. |
| `ADMIN_API_TOKEN` | the admin app proves itself to the API (16+ chars enforced) | API file AND admin file on the same VM | `openssl rand -hex 32` | Change both files at the same minute, restart both services. |
| `MONGO_PSEUDO_SECRET` | scrambles ids sent to Mongo (only if Mongo on) | VM file | `openssl rand -hex 32` | NEVER change it (every pseudonym would change). Back it up in your password manager. |
| `MONGODB_URI` | Atlas connection (only if Mongo on) | VM file | Atlas screen | Create a new database user, switch, delete the old user. |
| `ANTHROPIC_API_KEY` | optional story and question-draft writer | VM file | Anthropic console | Revoke and re-create in the console. Leave empty to use canned stories. |
| `APP_ENV`, `PORT`, `LISTEN_HOST`, `DB_PATH`, `TRUST_PROXY`, `ADMIN_ORIGIN`, `ALLOWED_ORIGINS`, `WEBAUTHN_RP_ID`, `WEBAUTHN_ORIGINS`, `WEBAUTHN_RP_NAME`, `FEATURE_WELCOME_GIFT`, `MEDIA_DIR`, `NARRATOR_MODEL`, `ASSET_BASE_URL` | settings, not secrets | VM file | see `deploy/oracle/env.example` | when the domain changes |
| other optional knobs | `RATE_LIMIT_IP_PER_MIN` (600), `MAX_CONCURRENT_REQUESTS` (256), `ACCESS_TTL_SECONDS` (900), `CHILD_TTL_SECONDS` (28800), `REFRESH_TTL_SECONDS` (30 days), `REQUEST_TIMEOUT_SECONDS` (10), `MEDIA_BASE_URL`, `LLM_*` | VM file | defaults are the contract values | leave alone |

### 8.2 Web app (`/etc/numerix/web.env`)

| Name | Purpose | Notes |
|---|---|---|
| `API_URL` | where the web server finds the API | `http://127.0.0.1:8081/v1` on both VMs. The same value is baked in at build time for image rewrites, so ONE build runs on staging and production. No secret. |
| `NEXT_PUBLIC_*` | on/off switches and language defaults | Baked in at build from `config/env/production.env`. Not secret. They are the same for staging and production (the only use of `NEXT_PUBLIC_APP_ENV` in the code is a dev-only mascot warning). |

### 8.3 Admin app (`/etc/numerix/admin.env`)

| Name | Purpose | How generated | Rotation |
|---|---|---|---|
| `ADMIN_PASSWORD` | the one admin login (8+ chars enforced; use 20+) | password manager | change the file, restart; sessions stay valid until `ADMIN_SESSION_SECRET` changes |
| `ADMIN_SESSION_SECRET` | signs the admin session cookie (32+) | `openssl rand -hex 32` | change and restart: admin signs in again |
| `ADMIN_API_TOKEN` | same value as the API's | see 8.1 | see 8.1 |
| `PLATFORM_API_URL` | where the API is, no `/v1` | `http://127.0.0.1:8081` | none |

### 8.4 Backups

| Name | Purpose | Where set |
|---|---|---|
| `LITESTREAM_ACCESS_KEY_ID`, `LITESTREAM_SECRET_ACCESS_KEY` | write access to the Oracle bucket | `/etc/litestream.env` on the VM (create the key pair in Oracle: Profile, Customer Secret Keys) |
| B2 application key (id and key) | write access to the Backblaze bucket | same file, with its own names if you add the second replica (section 9) |

### 8.5 GitHub (set in the GitHub website; Settings, Secrets and variables, Actions)

Create two **Environments** in EACH repo: `staging` and `production`. The same names are used in both repos and both environments, each with different values.

| Kind | Name | Environment | Meaning |
|---|---|---|---|
| Secret | `DEPLOY_SSH_KEY` | staging, production | PRIVATE half of that environment's deploy key (one key pair per environment) |
| Secret | `DEPLOY_KNOWN_HOSTS` | staging, production | output of `ssh-keyscan -t ed25519 <vm-ip>` for that VM, after you checked the fingerprint |
| Secret (optional) | `SMOKE_ADMIN_API_TOKEN` | staging only | lets the API smoke test check the admin API; do NOT set on production |
| Variable | `DEPLOY_HOST` | staging, production | the VM address |
| Variable | `DEPLOY_USER` | staging, production | `deploy` |
| Variable | `TARGET_ARCH` | staging, production | `arm64` (Oracle Ampere) or `amd64` (most paid VPS). API repo only. Default `arm64` |
| Variable | `API_URL` | staging, production | `https://staging-api.YOURDOMAIN` / `https://api.YOURDOMAIN` (API repo) |
| Variable | `WEB_URL`, `ADMIN_URL` | staging, production | `https://staging-app...` / `https://app...`, and the admin ones (web repo) |
| Variable (repository) | `PROD_APPROVAL_CONFIRMED` | repository | set to `yes` ONLY after step G2 (reviewers) is done; production deploys refuse to start without it |
| Variable (repository, optional) | `WEB_REPO`, `WEB_REF` | API repo | which repo and branch holds the contract the API is generated from (defaults `Perrin-Nj/numerix-web`, `main`) |
| Variable (repository, optional) | `API_REPO` | web repo | the owner of `numerix-api` for the full-suite job |
| Secret (only if repos are private) | `WEB_REPO_TOKEN`, `API_REPO_TOKEN` | repository | a fine-grained token with read access to the other repo, used only by CI to check out the other side |
| Variable (optional) | `KEEPALIVE_URLS` | web repo | space-separated health URLs for the 5-minute check |

Switching hosts later (for example Oracle to OVH) means changing `DEPLOY_HOST`, `TARGET_ARCH`, the two secrets and the DNS records, and running `setup.sh` on the new machine. No workflow changes. The workflows only speak SSH to the `numerix-deploy` gate.

### 8.6 Private repositories and the approval gate (decision D1 for you)

VERIFIED 2026-10-21 against GitHub's docs (https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments): on GitHub Free, Pro and Team, "required reviewers are only available for public repositories", and environment secrets work in private repositories only on paid plans (Pro, Team, Enterprise). Whether Enterprise adds reviewers for private repositories is not stated on that page: UNVERIFIED.

So the manual "press Approve" gate you asked for needs ONE of:

1. **Make both repositories public.** Nobody can read your secrets (they are not in the repo; CI scans for them), but your code, question bank and plans become public. Decide whether you accept that.
2. **Upgrade the GitHub account or organisation to a plan that supports required reviewers on private repositories.** Check the current plan page and price first: UNVERIFIED.
3. **Weaker fallback** (ask Claude to build it): keep the repos private, store the deploy secrets as repository secrets with `PROD_` and `STAGING_` prefixes, and make production deploys run only from "Run workflow" with a typed confirmation `DEPLOY v1.2.3` and your account name. That is a deliberate action by you, but it is not a second person's approval and it is not enforced by GitHub for other collaborators. Fine while you are the only contributor.

Until you decide, the production workflow refuses to run (`PROD_APPROVAL_CONFIRMED` is not `yes`).

---

## 9. Backups and the restore drill

What exists:

- **Continuous copy** of the database with Litestream to an Oracle bucket (`/etc/litestream.yml`, path `prod/numerix.db` or `staging/numerix.db`). Retention in the file: 168 hours (7 days) of history. Litestream version: the setup script downloads the latest `.deb` from GitHub; the configuration keys in the repo file were not re-checked against that newest version: UNVERIFIED, verify by running the restore drill below right after setup.
- **Second off-site copy (recommended for production):** in `litestream.yml` add a second `replicas:` entry of type `s3` pointing at a Backblaze B2 bucket (B2 offers an S3-compatible endpoint; exact endpoint format for your region: UNVERIFIED, shown on the B2 bucket page). B2 free tier covers 10 GB (VERIFIED).
- **Before every production deploy and every staging deploy:** `numerix-deploy backup` makes a consistent copy (`VACUUM INTO`, integrity checked) in `/var/lib/numerix/backups`, last 14 kept.
- **Before every content seed:** the seed tool makes its own backup.
- **Daily restore check:** `numerix-verify-backup.timer` (04:30) restores the newest replica into a temp folder and runs the integrity check. If it fails, `systemctl status numerix-verify-backup` shows it. It also keeps the machine slightly busy, which helps a little against Oracle's idle rule but is not a guarantee.
- **Not backed up by Litestream:** the 200 Discover files (immutable, named by content hash). Keep the originals in the repo (`seed/feed-media`, 45 MB) and in your own storage.
- **Mongo Atlas M0 has no backups.** It can always be rebuilt from SQLite (docs/MONGO_SYNC.md).

Restore drill (do it once on staging BEFORE go-live, then once on production right after the first real data, then whenever the daily check fails):

1. On the VM: `litestream restore -config /etc/litestream.yml -o /tmp/restored.db /var/lib/numerix/numerix.db`.
2. `sqlite3 /tmp/restored.db "PRAGMA integrity_check; SELECT COUNT(*) FROM bank_questions;"` must print `ok` and about 1229 (published plus retired rows, `RELEASE_CHECKLIST.md` section 4).
3. Full rehearsal (staging only): stop the API, move the database away, restore into place, start the API, run the smoke test.
4. Write the date of the drill in `docs/PLAN.md`.

---

## 10. Email and Mongo

**Email.** Searched both repos 2026-10-21: no code sends email. Consequences: there is no "forgot password" or "verify email" yet (BE-34 and BE-35 in `contracts/BACKEND_INTEGRATION.md` are open). A parent who forgets the password cannot reset it by email today. Before public launch decide: build BE-34/35 (then a sender is needed, cost UNVERIFIED) or accept a manual support path. This is a launch-readiness decision for you, not a hosting problem.

**MongoDB Atlas M0 (optional, off by default).** Skip it at launch: SQLite serves everything and the app logs "mongo sync is off". If you turn it on: M0 free, 512 MB, no backups; choose a Canadian region if offered (UNVERIFIED on the free tier); restrict network access to the VM IP, never `0.0.0.0/0`; database user with `readWrite` on `numerix` only. Steps are in `DEPLOY_ORACLE.md`.

---

## 11. Monitoring and alerts (free path)

| What | How | Cost |
|---|---|---|
| Is it up? | `GET https://api.YOURDOMAIN/v1/health` (returns status and version), `GET https://app.YOURDOMAIN/api/health` (says which side is down), `https://admin.YOURDOMAIN/login` | free |
| Who checks? | The repo workflow `keepalive.yml` pings every 5 minutes from GitHub and fails when something is down (GitHub emails the owner on failure). Plus one external free monitor with email or phone alerts: UptimeRobot, Better Stack or Healthchecks.io (terms UNVERIFIED, section 4). | $0 |
| Certificate and disk | smoke tests check the certificate; add a monitor on the same URLs (a failing certificate shows as down) | $0 |
| Backups | `systemctl status numerix-verify-backup` and `litestream snapshots -config /etc/litestream.yml /var/lib/numerix/numerix.db` | $0 |
| Logs | `journalctl -u numerix-api -f` (JSON lines: request id, status, duration; no names, emails or answers by design), `journalctl -u numerix-web`, `journalctl -u caddy` | $0 |
| Admin view | `GET /admin/v1/health` with the admin token reports Mongo sync state | $0 |
| Oracle emails | read them: they warn before reclaiming | $0 |

---

## 12. Rollback, by component

All deploys keep the last five releases on the VM. Every production deploy takes a database backup first.

| Component | How to go back | Time | Data effect |
|---|---|---|---|
| API (binary) | GitHub: Actions, `rollback`, Run workflow, choose environment, leave "restore database" off. Or on the VM: `sudo -u deploy numerix-deploy rollback api` | about 15 s of downtime | none |
| API plus database | same workflow with "restore database" ticked (or `numerix-deploy restore-db latest`) | about 1 minute | **everything written since the backup is lost**. Use only when a migration or a bad deploy corrupted data. The automatic path does this only when the new release never became healthy (nobody was served, so nothing is lost). |
| Web or admin | Actions, `rollback` (web repo), choose environment and component | about 5 s | none: the web and admin apps hold no data of their own |
| Content (questions, worlds) | `bash scripts/db-prod-deploy.sh --rollback <run_id>` on the VM (restores questions to their pre-run state as a new version); worlds, quests and assets need a backup restore | minutes | questions only |
| Whole machine lost | new VM, run `setup.sh`, restore the database from the bucket (section 9), set DNS, deploy the last tag | 1-2 hours | up to a few seconds of writes lost (Litestream) |
| DNS or certificate problem | check DNS records first; Caddy log `journalctl -u caddy` | | none |

Migrations run automatically when the new API binary starts. They only move forward. An older binary may refuse or misbehave on a newer database, which is why a rollback after a migration may need the pre-deploy backup. Rehearse each release with a copy of production data on staging when the release contains a new migration (list: `ls internal/store/migrations`).

---

## 13. Children's data, Law 25 and PIPEDA: legal readiness

I am not a lawyer and none of this is legal advice. These are owner and legal tasks, with what the app already does.

What the app already does (from `docs/SCHEMA.md`, `contracts/BACKEND_INTEGRATION.md`, `docs/MONGO_SYNC.md`; I did not audit every code path):

- A child profile cannot play until a parent consent exists. The consent record keeps parent id, child id, notice version, a hash of the notice text, UTC time, a truncated IP network and the user agent family; kept while the child exists and two years after deletion.
- Consent covers children under 14; revoking equals deleting.
- Endpoints exist for the parent to export a child's data (`GET /children/{id}/export`) and to delete a child or the account (`DELETE /children/{id}`, `DELETE /account`).
- Logs carry a request id and no child name, email or answer. Mongo gets only pseudonymised ids, never names, emails, birth years or free text, and only statistics with 5 or more children.
- Admin reads and changes of child data are audited.
- Passwords and kid PINs are hashed (argon2id).

What you (the owner) must do, with a lawyer where marked:

1. **Name a person in charge of personal information** (Law 25 requires one; by default the head of the business). Publish their contact on the site. (Source for the requirement: Quebec private sector law summaries, e.g. https://www.cfib-fcei.ca/fr/loi-25-quebec; read the official text or the CAI site.)
2. **Privacy policy and consent text**, in French and English, that match what the app really collects. The consent notice is versioned in the app (`notice_version`); the real text must be written and approved. LAWYER.
3. **Parental consent under 14**: Quebec law requires the parent's or guardian's consent to collect information from a child under 14 (summary from https://www.cai.gouv.qc.ca/ and law-firm notes found 2026-10-21). The app has the consent step; confirm with a lawyer that its wording and flow are enough. LAWYER.
4. **Privacy impact assessment** before data leaves Quebec or before you launch a service that collects personal information from children. Hosting in Quebec (Oracle Montreal, OVH Beauharnois) removes the cross-border question for the main database; the Backblaze copy and the optional Atlas cluster need a decision and a written record.
5. **Confidentiality incident plan**: Law 25 requires measures to reduce harm, a notice to the CAI (Commission d'accès à l'information) and to the people concerned when there is a risk of serious harm, and an incident register that the CAI can ask for (summary from the same sources). Write a one-page procedure: who decides, who calls whom, a register template. Pre-write the notice. LAWYER.
6. **Retention and deletion**: decide how long accounts are kept after inactivity and how deletion requests are answered (time limit). The app deletes on request; there is no automatic "delete after N months" job today.
7. **Vendors**: list every service that touches the data (Oracle or OVH, Backblaze, GitHub for code only, Anthropic only if story writing is switched on, Atlas only if Mongo is on). The Anthropic story writer sends text to a third party: leave it OFF until the policy covers it (the canned stories are the default).
8. **PIPEDA** (federal) applies to cross-border and interprovincial commercial activity; Quebec families fall under Law 25 first. Ask your lawyer which applies to your plans.
9. **Terms of use and payments**: required before charging anyone. The payments part is outside this package (no payment provider is configured here: UNVERIFIED, check `docs/PLAN.md` section on billing).

---

## 14. Capacity and load numbers (from `docs/LOAD_TEST.md`, run on a busy Windows laptop)

- Design targets: answer under 400 ms, next-question under 800 ms, health under 100 ms at the 95th percentile; about 200 kids at the same time.
- The test tool is `cmd/loadtest`. On the shared laptop, p50 was 1-3 ms in every run; the 95th percentile missed the targets in most runs because the laptop itself stalled for 1 to 1.7 seconds (the test's own timer woke late). Zero server errors and zero database-locked errors in every run. The document's verdict: "targets are NOT reliably met on this machine; re-run on a quiet Linux host".
- A quiet-host number does not exist yet. **Staging run is the gate**: on the staging VM run `bin/loadtest -rps 50 -duration 20s` and require zero 5xx and answer p95 under 400 ms (as `RELEASE_CHECKLIST.md` says). Until that is done, the capacity claim is UNVERIFIED.
- Known limit: SQLite has one writer; the first real ceiling is expected above a few hundred requests per second. The rate limiter is per process. One API process per environment is the supported shape.
- A 1 OCPU / 8 GB Oracle VM or a 2 vCore / 4 GB OVH VM is far above what a launch needs if those numbers hold: UNVERIFIED until measured on the VM.

---

## 15. Config readiness review (what I read in the code and what I changed)

Read: `internal/config/config.go`, `cmd/api/main.go`, `internal/httpapi` (CORS, client IP), `internal/app` (cookies, passkeys), web `next.config.ts`, `vercel.ts`, the API proxy route, admin config.

Works already:

- `APP_ENV` allows `development`, `test`, `production`; production refuses a missing or short `JWT_SECRET`, a short admin token, `PPROF`, and a Mongo URI without its pseudonym secret.
- CORS answers only origins listed in `ALLOWED_ORIGINS` (API) and `ADMIN_ORIGIN` (admin API); no wildcard.
- The API device cookie `nx_device` is `HttpOnly`, `SameSite=Lax`, `Secure` outside development. The web app's cookies are `Secure` when `NODE_ENV=production`, which every `next build` output is, so staging must also be HTTPS (it is).
- Passkeys need `WEBAUTHN_RP_ID` and `WEBAUTHN_ORIGINS`; the library compares the origin exactly.
- Cookies carry no `Domain` attribute, so each environment's cookies stay on their own host (staging and production never share a login).

Gaps found and FIXED in this branch (with tests):

1. **Every family would have shared one rate limit (important).** The web server called the API without telling it the visitor's address, so with `TRUST_PROXY=true` the API saw one address (the web server) for everybody: 600 requests a minute and 5 sign-ups an hour for ALL users together. Fix: the web proxy now forwards the visitor's address from the host in front of it (`src/shared/client-ip.ts`, 7 checks in `scripts/check-client-ip.mjs`). The API trusts that header only because it listens on `127.0.0.1`, so nobody can reach it directly to forge it. The same problem would have existed on Vercel.
2. **The API listened on all network interfaces.** With `TRUST_PROXY=true` and a reachable port, a visitor could forge their address. Fix: new `LISTEN_HOST` setting (default empty = unchanged behaviour); the production file sets `127.0.0.1`.
3. **Silent mistakes in production origin settings** (http instead of https, `localhost`, a path, a passkey id without origins so passkeys quietly stay off, origins outside the passkey id). Fix: `Load` now rejects them in production (table test `TestProductionOrigins`).
4. **`config/env/production.env` could not build.** It set `NEXT_PUBLIC_MASCOT_KIND=emoji`, which the app rejects (`image`, `owl` or `rive`), so `next build` failed. Fixed to `owl` in the three env files. Found by running the real self-host build.
5. **Security headers existed only on Vercel** (`vercel.ts`). Added the same headers to the self-host build (`next.config.ts`, switched on by `NEXT_OUTPUT=standalone`); `scripts/smoke-web.sh` checks them on the live host.
6. **Image resizing** needs a native library that would not match an ARM VM when built on an x86 runner. The self-host build serves the already-sized pictures as they are (`images.unoptimized`); Vercel builds are unchanged.

Gaps found and NOT fixed (decisions or larger work):

- No email, no password reset or email verification (section 10).
- `.env.example` is missing from the web repo (it is ignored by `.gitignore`), which `scripts/vercel-env.mjs` needs. Only matters if you ever use Vercel again.
- The web app's per-IP admin login limit and the API's limiter live in memory of one process; fine for one process per environment.
- Passkey RP id is one host. Moving the web app to another host name later invalidates every passkey.
- "Same artifact" is exact for the API (one binary per CPU type) and for web and admin (one archive; nothing environment-specific inside). It holds only because the API address `127.0.0.1:8081` is the same on every VM.
- Self-hosted Next.js with `next/image` unoptimized and the video route was tested locally only (section 21).
- Discover items are loaded with `cmd/feedseed`, which is part of the seed bundle but not automated in the workflow: run it by hand after the first deploy (step R13).

---

## 16. The pipeline (GitHub Actions)

Files, in BOTH repos under `.github/workflows` (and a shared step in `.github/actions/vm-ssh`):

| File | When | What it does |
|---|---|---|
| `ci.yml` | every pull request, every push to `main`, and called by deploy-staging | API: `go vet`, `gofmt`, `staticcheck`, tests with the race detector, `govulncheck`, arm64 build, seed validation, deploy-gate test, load smoke, contract drift against the web repo, secret scan, workflow lint. Web: contract drift (`pnpm gen:api` then `git diff`), copy / sounds / art / client-IP checks, architecture check, typecheck, lint, build, self-host build and start, Storybook build, admin typecheck, lint and build, the full test suite, `pnpm audit`, secret scan, workflow lint |
| `deploy-staging.yml` | push to `main` | runs `ci.yml`, builds ONCE (API for arm64 and amd64; web and admin archives), stores the build as a 90-day artifact named `release-<commit>`, deploys to staging, health check, smoke test with a real sign-up and delete, applies the content seed (API repo), rolls back and fails loudly if anything fails |
| `deploy-prod.yml` | push of a tag `vX.Y.Z`, or Run workflow | refuses unless the tag is on `main`, a staging deploy of that exact commit succeeded and `PROD_APPROVAL_CONFIRMED=yes`; then WAITS for a human to approve (environment `production`); downloads the SAME artifact staging used and checks its checksums; backs up the database; installs; health check; smoke; automatic rollback |
| `rollback.yml` | Run workflow | puts the previous release back (API: optionally restores the newest backup) and runs the smoke test |
| `db-content.yml` | Run workflow (API repo) | content seed: dry run, then apply only if ticked; production waits for approval |
| `keepalive.yml` (web repo) | every 5 minutes | pings the health URLs in `KEEPALIVE_URLS` |

Checked 2026-10-21: `actionlint` 1.7.7, `yamllint` 1.35 and `shellcheck` give no findings on these files. Pinned tool versions: staticcheck v0.8.2, govulncheck v1.9.0, oapi-codegen v2.8.0, Node 22, pnpm from `packageManager`. Third-party actions are pinned by major tag (`@v4`, `@v5`); Dependabot (weekly) is configured for them. Whether newer major versions exist: UNVERIFIED.

Honest limits: the YAML has been linted but NEVER RUN on GitHub (no remote access from here). The first staging run is the real test of the workflow files. The "same artifact" promise: staging and production use the exact files (checksum compared); artifacts expire after 90 days, after which a tag must wait for a fresh staging run.

Order of a release when both repos change: merge API, wait for its staging deploy; merge web, wait for its staging deploy; test staging by hand; tag the API (approve); tag the web (approve). The API is backward compatible with the previous web release for one version (contract rule); do not skip this order.

---

## 17. Go-live runbook

Do the steps in order. After each step compare with "Expect". If it does not match, stop and read the troubleshooting hint; do not continue.

**Part G: accounts and decisions (no servers yet)**

- **G1. Decide D1 (section 8.6) and D2 (hosting: production VM provider).** Expect: you wrote both decisions in your notes.
- **G2. GitHub environments.** In each of the two repos: Settings, Environments, New environment, name `staging` (no rules). Again, name `production`, tick "Required reviewers", add yourself, Save protection rules. Then Settings, Secrets and variables, Actions, Variables, New repository variable `PROD_APPROVAL_CONFIRMED` = `yes`. Expect: environment `production` shows "Required reviewers: 1". If the option is greyed out, you are on D1 option 3 or need a plan change.
- **G3. Buy the domain** (section 6). Expect: you can open the registrar's DNS page.
- **G4. Oracle account** (and/or OVH account): create, choose the home region Canada Southeast (Montreal). Expect: you reach the console. If sign-up is refused, switch to OVH for both VMs.

**Part R: servers**

- **R1. Create the two VMs.** Oracle: Menu, Compute, Instances, Create instance. Name `numerix-staging`, image Ubuntu 24.04, shape Ampere `VM.Standard.A1.Flex` 1 OCPU / 4 GB, paste your SSH public key, create. Repeat for `numerix-prod` with 1 OCPU / 8 GB (or rent the OVH VPS). Expect: two machines in state Running with public IPs. If "out of capacity", retry later or try another availability domain.
- **R2. Open ports.** Section 6 item 4, on the network of each VM. Expect: you can still SSH.
- **R3. DNS records** (section 6). Expect, from your PC, `nslookup app.YOURDOMAIN` returns the production IP (may take a few minutes).
- **R4. Deploy keys.** On your PC, twice (staging and production):
  `ssh-keygen -t ed25519 -f nx_deploy_staging -C numerix-staging-deploy -N ""` and the same for `nx_deploy_production`.
  Expect: four files. The `.pub` files are public. The files without `.pub` are PRIVATE: they go only into GitHub secrets (R7) and are then deleted from your PC.
- **R5. Run the setup script on each VM.** Copy the folder: `scp -r deploy/oracle ubuntu@VMIP:~/` and `scp nx_deploy_staging.pub ubuntu@VMIP:/tmp/deploy.pub`. Then `ssh ubuntu@VMIP` and run
  `sudo bash ~/oracle/setup.sh --app staging-app.YOURDOMAIN --admin staging-admin.YOURDOMAIN --api staging-api.YOURDOMAIN --deploy-key /tmp/deploy.pub`
  (production: the `app.`, `admin.`, `api.` names and the production public key). Expect the last line "setup done". If Litestream download fails, install it by hand from its releases page.
- **R6. Fill the settings on the VM.** `sudo nano /etc/numerix/api.env`: set `JWT_SECRET`, `ADMIN_API_TOKEN` (run `openssl rand -hex 32` twice on the VM), the five origin and passkey lines for that environment (see `env.example`; staging uses the `staging-` names). `sudo nano /etc/numerix/admin.env`: same `ADMIN_API_TOKEN`, a long `ADMIN_PASSWORD`, `openssl rand -hex 32` for `ADMIN_SESSION_SECRET`. `sudo nano /etc/litestream.yml`: replace `__ENV__` with `staging` or `prod`, `__NAMESPACE__`, `__REGION__`. `sudo nano /etc/litestream.env`: the two bucket keys. Then `sudo systemctl restart litestream`. Expect: `systemctl status litestream` says active (it may wait for the database to exist).
- **R7. GitHub secrets and variables** (section 8.5), in both repos, both environments, using the GitHub website. Get the host key line with `ssh-keyscan -t ed25519 VMIP` on your PC, and compare its fingerprint with `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` run on the VM. Expect: all names in the list exist (values are hidden).
- **R8. Copy the Discover media once.** `scp -r seed/feed-media ubuntu@VMIP:/tmp/` then on the VM `sudo mv /tmp/feed-media /var/lib/numerix/feed-media && sudo chown -R numerix:numerix /var/lib/numerix/feed-media`. Expect: 200 files plus captions.

**Part S: staging rehearsal**

- **S1. First staging deploy.** Merge the branch `prod/go-live` to `main` in the API repo, then the web repo. Watch Actions, workflow `deploy-staging`. Expect: all jobs green; the summary shows the release id; the log ends with `SMOKE PASSED`. If the SSH step fails: wrong key, host key line or `DEPLOY_HOST`.
- **S2. Load the Discover items on staging.** SSH to the staging VM, then `ls /opt/numerix/seed/releases/` (note the newest id) and run `cd /opt/numerix/seed/releases/<id> && sudo -u numerix ./bin/feedseed --db /var/lib/numerix/numerix.db --items seed/feed/items.json --media /var/lib/numerix/feed-media` (the seed bundle includes `feedseed`). Expect: it validates, then reports the items loaded; running it twice changes nothing.
- **S3. Look at staging with your own eyes.** Open https://staging-app.YOURDOMAIN, register a parent, add a child, play a chapter, open Discover (videos play, scrubbing works), try a passkey. Open https://staging-admin.YOURDOMAIN and sign in. Expect: everything works, no browser warning on the padlock.
- **S4. Load test on staging.** `bin/loadtest -rps 50 -duration 20s` against the staging API. Expect: zero 5xx, answer p95 under 400 ms. If not, stop: capacity is not proven.
- **S5. Restore drill on staging** (section 9). Expect: `ok` and about 1229.
- **S6. Rollback drill on staging.** Actions, `rollback`, environment staging. Expect: green, and the smoke test passes. Then run `deploy-staging` again (Run workflow) to roll forward.
- **S7. Failed-deploy drill (optional but recommended).** Temporarily set the staging variable `API_URL` to a wrong address and run deploy-staging; expect it to fail loudly with a red cross. Fix the variable.

**Part P: production**

- **P1. Tag the API.** On the `main` commit that passed staging: `git tag v1.0.0` then `git push origin v1.0.0` (API repo). Expect: Actions shows `deploy-prod` waiting with "Review pending".
- **P2. Approve.** Open the run, click "Review deployments", tick `production`, "Approve and deploy". Expect: backup step prints a backup file name, install, health, `SMOKE PASSED`, summary says success. If it fails the job rolls back by itself; read the red step.
- **P3. Seed the production content.** Actions, `db-content`, environment production, release = the id from the summary, leave "apply" OFF first. Approve. Read the dry run (counts of added, changed, unchanged). Run again with "apply" ON. Expect: about 20 worlds, 89 quests, 1229 questions added the first time, then "unchanged" on a re-run.
- **P4. Discover items on production** (as S2, on the production VM).
- **P5. Tag the web repo** `v1.0.0`, approve. Expect: `SMOKE PASSED` for web and admin.
- **P6. Smoke test checklist by hand** (section 18).
- **P7. Restore drill on production** (section 9).
- **P8. Turn on monitoring** (section 11). Expect: an alert email arrives when you stop the web service for one minute as a test (`sudo systemctl stop numerix-web`, then start it again).
- **P9. Announce** only after the 24-hour watch list below has started.

---

## 18. Smoke test checklist (after every production deploy)

Automatic (`scripts/smoke-prod.sh` for the API, `scripts/smoke-web.sh` for web and admin): health with the expected version; database ready; auth enforced; admin API closed without a token; pprof closed; certificate valid 14+ days; pages 200; security headers; the proxy reaches the API; (staging only) sign up, session, delete.

By hand, 10 minutes, production, with a real test parent account that you delete afterwards:

- [ ] Home page loads in French and English, no padlock warning.
- [ ] Register, log in, log out, log in again.
- [ ] Add a child, give consent, play one full chapter; answers save.
- [ ] Discover feed: a video plays, seeking works, photos show.
- [ ] A passkey can be added and used (works only on the exact `app.` address).
- [ ] Admin login works; Audit shows your actions; the config page saves one harmless change and the app reads it within a minute.
- [ ] Export the child's data; delete the child; delete the account.
- [ ] `curl -m 5 http://PRODUCTION_IP:3000` and `:8081` fail (not reachable from outside).
- [ ] An uptime alert reaches your phone.

---

## 19. First 24 hours watch list

- Every hour for the first 3 hours, then every 4: `journalctl -u numerix-api --since "1 hour ago" | grep -c '"level":"ERROR"'` should stay at zero or tiny.
- Disk: `df -h /var/lib/numerix` below 70 percent.
- Memory: `systemctl status numerix-web numerix-admin` memory well under the 900 MB cap (a restart loop means it is hitting it).
- Backups: `litestream snapshots ...` shows a snapshot from the last hour; next morning the 04:30 restore check passed.
- Certificates: padlock fine on all three public names.
- Rate limits: no wave of `429` in the API log from normal families (this is the check for fix 1 in section 15).
- Sign-ups arriving, no database-locked errors, no 5xx.
- Oracle or OVH emails: read them.
- Costs: nothing unexpected billed (OVH invoice, Backblaze usage page).
- Keep the previous release on the VM (it is kept automatically) and do not deploy anything else during these 24 hours except a rollback.

---

## 20. Checklists for you

### 20.1 What I (the owner) must provide

Never paste a secret into chat. Where a secret is needed, the instruction says which place (a GitHub secret name, a file on the VM).

- [ ] Your decision on D1 (public repos, paid plan, or the weaker fallback) and D2 (production host: Oracle free, or OVH paid).
- [ ] The domain name you bought.
- [ ] The public IP of each VM, and the VM architecture (`arm64` or `amd64`).
- [ ] Oracle namespace and region (shown in the console, Profile, Tenancy) for the bucket address.
- [ ] Confirmation that DNS records exist.
- [ ] Confirmation that GitHub secrets and variables are set (names only).
- [ ] The name and contact of your person in charge of privacy, and the dates of your lawyer review.
- [ ] Which monitoring service you chose (after reading its terms).
- [ ] Decision on email (section 10) and on the Anthropic story writer (default OFF).

### 20.2 What I (the owner) must do by hand

The click-level lists are in section 17 (G2, G4, R1, R2, R4-R8) and in `PROD_OWNER_CHECKLIST.md`. Short form:

1. GitHub: two environments per repo, reviewers on `production`, `PROD_APPROVAL_CONFIRMED=yes`.
2. Registrar: buy the domain, add the six A records.
3. Cloud: create two VMs, open ports 80/443/22.
4. Your PC: create two deploy key pairs; paste each PRIVATE key into the GitHub secret `DEPLOY_SSH_KEY` of the matching environment; delete the local private files afterwards.
5. VMs: run `setup.sh`, then fill `/etc/numerix/*.env`, `/etc/litestream.yml`, `/etc/litestream.env`.
6. Press Approve on the production deploys.
7. Run the manual smoke checklist and the restore drill.

### 20.3 What Claude does once it has received your items

- Fills in the example files with your real (non-secret) domain names, opens a pull request in each repo, and merges to `main` when you say so.
- Reads each GitHub Actions run, fixes workflow or script problems, and re-runs.
- Checks the DNS and certificates from the outside (read-only `curl`/`nslookup`/`openssl s_client`).
- Runs the smoke scripts against staging and reports.
- Writes the incident procedure and privacy register templates for your lawyer to edit.
- Helps read the dry-run output of the content seed before you approve.
- Does NOT hold or ask for passwords, private keys or tokens, and does not press the production Approve for you.

---

## 21. Verification gates (why failure becomes visible and reversible)

Certainty of a flawless launch is impossible. These gates make failure early, visible and reversible:

1. CI on every change (tests, race detector, type checks, security scan, contract drift).
2. Staging rehearsal of every release with a real sign-up and delete.
3. Dry run before every content change; automatic backup before every apply and every deploy.
4. Same artifact from staging to production, checksums compared.
5. A human approval before production.
6. Automatic health check and smoke test with automatic rollback.
7. Restore drill (staging before launch, production after first data) and a daily automatic restore check.
8. Rollback drill on staging before go-live.
9. Load test on the staging VM.
10. A monitor that tells you before families do.

### UNVERIFIED items and how to verify each

| Item | How to verify |
|---|---|
| GitHub workflows have never run on GitHub | first staging run; fix what fails |
| Whether Enterprise (or another plan) gives required reviewers on private repos; the price | GitHub plans page; try creating the rule |
| OVH VPS-1 currency, 12-month commitment, region choice at order | the order screen before paying |
| Hetzner / Vultr / DigitalOcean / Contabo Canadian availability and current prices | their pricing pages |
| `.ca` price at Cloudflare Registrar; current Namecheap prices | the registrars' cart pages |
| UptimeRobot, Better Stack, Healthchecks.io commercial terms | their terms of service |
| Email sender cost | when BE-34/35 are built |
| Oracle sign-up rejection behaviour; whether an Always Free VM at low load is reclaimed | watch Oracle emails; keep the off-site backup |
| Litestream newest version vs the repo's config keys; B2 endpoint format; B2 region and data location | restore drill right after setup; B2 bucket page |
| Backups are not encrypted before upload | ask Claude to add client-side encryption if your lawyer requires it |
| Self-host Next.js (standalone) on the real ARM VM: video Range requests, passkeys, image paths | staging checks S3 |
| 200 concurrent kids on the real VM | step S4 |
| Cloudflare / Netlify compatibility with this app | only if you ever choose them: build a staging copy first |
| Law 25 specifics (age threshold wording, incident thresholds, penalties) | read the official text or ask the lawyer; the summaries I found are secondary |
| Refresh-token behaviour after a `JWT_SECRET` change | test on staging |
| Node and tool pins, action major versions | `dependabot` pull requests |
