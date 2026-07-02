# Deploying Quipnotes to Google Cloud (free-tier e2-micro VM)

This deploys the server to a single **Compute Engine `e2-micro` VM**, which is in
GCP's [Always Free tier](https://cloud.google.com/free/docs/free-cloud-features#compute)
(≈ **$0/month**) and is the right fit because the server keeps all game state
**in memory in one process** and uses **WebSockets** — it must be a single,
always-on instance (no scale-to-zero, no multiple replicas).

**How deploys work:** GitHub Actions builds a Docker image on every push to
`master`, pushes it to **Artifact Registry**, copies the deploy config
(`docker-compose.prod.yaml` + `Caddyfile`) to the VM, then SSHes in (over an IAP
tunnel — no public SSH port) and tells it to pull the new image and restart. The
image is **code only**; the proprietary `words.csv`, the optional `prompts.txt`,
and `.env` live on the VM (never sent from CI) and are mounted in at runtime.

So after the one-time setup below, changing the app **or** the compose/Caddy
config just needs a push to `master`. The manual `scp` in step 6 is only to
**bootstrap** the VM for its very first `docker compose up`.

**HTTPS:** a **Caddy** container sits in front of the Go server and terminates
TLS with automatic Let's Encrypt certificates (renewed automatically), so
players connect over `https://` / `wss://`. This **requires a domain name**
pointed at the VM — Caddy validates domain ownership with Let's Encrypt to issue
the cert, so a bare IP won't work. Steps 5–6 below cover reserving a static IP,
the DNS record, and the domain config.

---

## 0. Prerequisites (one-time, on your machine)

- Install the [gcloud CLI](https://cloud.google.com/sdk/docs/install) and log in:
  ```bash
  gcloud auth login
  ```
- A GitHub repo for the server (you have `github.com/eric-sims/quipnotes`).
- **A domain (or subdomain) you control** for the server, e.g. `api.example.com`.
  You'll point a DNS `A` record at the VM in step 5.

Set some shell variables you'll reuse (pick a globally-unique project id):

```bash
export PROJECT_ID="quipnotes-$RANDOM"   # must be globally unique
export REGION="us-central1"             # free-tier region: us-central1 | us-west1 | us-east1
export ZONE="us-central1-a"
export REPO="quipnotes"                 # Artifact Registry repo name
export VM="quipnotes-vm"
export DOMAIN="api.example.com"         # the (sub)domain that will serve the game
```

---

## 1. Create the project & link billing

```bash
gcloud projects create "$PROJECT_ID"
gcloud config set project "$PROJECT_ID"

# List your billing accounts, then link one (the free tier still requires a
# billing account on file, but the e2-micro VM stays within the free allowance).
gcloud billing accounts list
gcloud billing projects link "$PROJECT_ID" --billing-account=XXXXXX-XXXXXX-XXXXXX
```

---

## 2. Set up a budget + price alerts (do this before anything else)

The safest guardrail. This emails you as spend approaches a threshold so a
free-tier mistake can't quietly cost money.

**Console (easiest):** Billing → **Budgets & alerts** → **Create budget**:
- Scope: this project.
- Amount: a **specified amount of `$5`** (anything above ~$0 means you left the
  free tier).
- Alert thresholds: 50%, 90%, 100% (email at each). Optionally 100% of a
  *forecasted* amount too.
- These emails go to the billing account admins by default.

**CLI equivalent:**
```bash
gcloud services enable billingbudgets.googleapis.com
BILLING_ACCOUNT=$(gcloud billing projects describe "$PROJECT_ID" \
  --format='value(billingAccountName)' | sed 's#billingAccounts/##')

gcloud billing budgets create \
  --billing-account="$BILLING_ACCOUNT" \
  --display-name="quipnotes budget" \
  --budget-amount=5USD \
  --threshold-rule=percent=0.5 \
  --threshold-rule=percent=0.9 \
  --threshold-rule=percent=1.0 \
  --filter-projects="projects/$PROJECT_ID"
```

> Tip: also confirm you're on the free tier at Billing → **Free trial status /
> Cost breakdown**, and check the **Compute Engine → free tier usage** later.

---

## 3. Enable the APIs

```bash
gcloud services enable \
  compute.googleapis.com \
  artifactregistry.googleapis.com \
  iap.googleapis.com
```

---

## 4. Create the Artifact Registry repo (stores your images)

```bash
gcloud artifacts repositories create "$REPO" \
  --repository-format=docker \
  --location="$REGION" \
  --description="Quipnotes server images"
```

Storage under 0.5 GB is free; your image is small and old tags get pruned, so
this stays effectively free.

---

## 5. Reserve a static IP, create the VM, open the firewall, add DNS

### Reserve a static IP

Because the TLS cert is tied to your domain → IP, the IP must be stable. A static
IP that stays *attached* to a running free-tier VM is free (only unattached
reserved IPs are billed).

```bash
gcloud compute addresses create quipnotes-ip --region="$REGION"
export STATIC_IP=$(gcloud compute addresses describe quipnotes-ip \
  --region="$REGION" --format='value(address)')
echo "$STATIC_IP"
```

### Create the VM

Must be **`e2-micro`** in a free-tier region with a **standard** (not SSD) disk
≤ 30 GB. Debian 12 is used here; the reserved IP is attached with `--address`.

```bash
gcloud compute instances create "$VM" \
  --zone="$ZONE" \
  --machine-type=e2-micro \
  --image-family=debian-12 \
  --image-project=debian-cloud \
  --boot-disk-size=30GB \
  --boot-disk-type=pd-standard \
  --metadata=enable-oslogin=TRUE \
  --address="$STATIC_IP" \
  --tags=http-server
```

### Firewall

Allow web traffic on 80/443 (80 is needed for the Let's Encrypt challenge and the
http→https redirect; 443 tcp+udp for HTTPS and HTTP/3), and allow IAP's range to
SSH for deploys:

```bash
# Players reach the server over HTTP/HTTPS; 443/udp enables HTTP/3
gcloud compute firewall-rules create allow-web \
  --allow=tcp:80,tcp:443,udp:443 --target-tags=http-server --source-ranges=0.0.0.0/0

# IAP tunnel range → SSH (this is the only SSH ingress you need)
gcloud compute firewall-rules create allow-iap-ssh \
  --allow=tcp:22 --source-ranges=35.235.240.0/20
```

### DNS

At your domain registrar / DNS host, create an **`A` record** for `$DOMAIN`
pointing at `$STATIC_IP`. Verify it has propagated before deploying (Caddy will
fail to get a cert until it resolves):

```bash
dig +short "$DOMAIN"   # should print your STATIC_IP
```

---

## 6. Install Docker on the VM & lay out the data dir

SSH in (over IAP):

```bash
gcloud compute ssh "$VM" --zone="$ZONE" --tunnel-through-iap
```

Then, **on the VM**:

```bash
# Docker Engine + compose plugin
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker "$USER"   # log out/in (or reconnect) for this to take effect

# Let Docker pull from Artifact Registry using the VM's service account
gcloud auth configure-docker "${REGION}-docker.pkg.dev" --quiet
# (REGION here = your region, e.g. us-central1)

# App directory
sudo mkdir -p /opt/quipnotes/data
sudo chown -R "$USER" /opt/quipnotes
```

Copy the compose file **and the Caddyfile** up (run this **from your machine**, in
the repo root):

```bash
gcloud compute scp docker-compose.prod.yaml Caddyfile "$VM":/opt/quipnotes/ \
  --zone="$ZONE" --tunnel-through-iap
```

Create `/opt/quipnotes/.env` **on the VM** (note the `/data` paths — that's where
the data dir is mounted inside the container). Replace `DOMAIN` and `ACME_EMAIL`
with your real values — Caddy uses them to obtain the TLS cert. (The shell vars
you exported earlier live on your laptop, not the VM, so type the values here.)

```bash
cat > /opt/quipnotes/.env <<'EOF'
WORDS_FILE_PATH=/data/words.csv
PROMPTS_FILE_PATH=/data/prompts.txt
GIN_MODE=release
DOMAIN=api.example.com
ACME_EMAIL=you@example.com
EOF
```

Upload your proprietary `words.csv` (and optional `prompts.txt`) into
`/opt/quipnotes/data/` (**from your machine**):

```bash
gcloud compute scp data/words.csv "$VM":/opt/quipnotes/data/ \
  --zone="$ZONE" --tunnel-through-iap
# optional:
# gcloud compute scp data/prompts.txt "$VM":/opt/quipnotes/data/ --zone="$ZONE" --tunnel-through-iap
```

---

## 7. Grant the VM permission to pull images

The VM's default Compute service account needs read access to Artifact Registry:

```bash
PROJECT_NUMBER=$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:${PROJECT_NUMBER}-compute@developer.gserviceaccount.com" \
  --role="roles/artifactregistry.reader"
```

---

## 8. Create the deployer service account (used by GitHub Actions)

```bash
gcloud iam service-accounts create gh-deployer \
  --display-name="GitHub Actions deployer"

SA="gh-deployer@${PROJECT_ID}.iam.gserviceaccount.com"

# Push images
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:$SA" --role="roles/artifactregistry.writer"
# SSH into the VM via IAP and run the deploy command
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:$SA" --role="roles/compute.osAdminLogin"
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:$SA" --role="roles/iap.tunnelResourceAccessor"
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:$SA" --role="roles/compute.viewer"

# Create a JSON key (this is the file you paste into GitHub as GCP_SA_KEY)
gcloud iam service-accounts keys create gh-deployer-key.json --iam-account="$SA"
```

> **Security note:** a downloaded JSON key is the simple path. The more secure
> upgrade is [Workload Identity Federation](https://github.com/google-github-actions/auth#preferred-direct-workload-identity-federation)
> (keyless). Fine to start with the key and switch later. Keep
> `gh-deployer-key.json` out of git — delete it after pasting into GitHub.

---

## 9. Configure GitHub

In the repo: **Settings → Secrets and variables → Actions**.

**Secrets** (encrypted):

| Name         | Value                                        |
|--------------|----------------------------------------------|
| `GCP_SA_KEY` | full contents of `gh-deployer-key.json`      |

**Variables** (Repository variables tab):

| Name             | Example           |
|------------------|-------------------|
| `GCP_PROJECT_ID` | `quipnotes-12345` |
| `GAR_LOCATION`   | `us-central1`     |
| `GAR_REPOSITORY` | `quipnotes`       |
| `GCE_INSTANCE`   | `quipnotes-vm`    |
| `GCE_ZONE`       | `us-central1-a`   |

---

## 10. Deploy

Push to `master` (or run the **Build & Deploy** workflow manually from the
Actions tab). The workflow builds, pushes, and restarts the container on the VM.

Verify over HTTPS (give Caddy ~30s on first boot to obtain the cert):

```bash
curl "https://$DOMAIN/games/0000"
# 404 = server is up, TLS is working, and it's answering (unknown game code)
```

If the cert isn't issued, check Caddy's logs on the VM:
`cd /opt/quipnotes && docker compose -f docker-compose.prod.yaml logs caddy`
— the usual cause is DNS not yet pointing `$DOMAIN` at the VM's IP, or port 80
being unreachable (Let's Encrypt's HTTP challenge needs it).

---

## Notes & follow-ups

- **HTTPS / mixed content:** solved by the Caddy container — clients reach the
  game at `https://$DOMAIN` and open the WebSocket at `wss://$DOMAIN/games/:code/events`.
  Point your Vue clients' `VITE_API_URL` at `https://$DOMAIN` (not the bare IP).
- **Certs persist:** issued certs live in the `caddy_data` Docker volume, so
  restarts/redeploys don't re-request them (which avoids Let's Encrypt rate
  limits). Renewal is automatic.
- **Static IP:** reserved in step 5 and attached to the VM. It's free while
  attached to a running VM; if you delete the VM, also release the address
  (`gcloud compute addresses delete quipnotes-ip --region="$REGION"`) so an
  idle reserved IP isn't billed.
- **Egress limit:** the free tier includes 1 GB/month of North America egress —
  plenty for a text-based game, but worth knowing.
- **Restart on reboot:** `restart: unless-stopped` in the compose file brings the
  container back after a VM reboot.
