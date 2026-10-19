#!/usr/bin/env bash
#
# Provision (or update) the Azure Container Apps environment for ScaleOfCoherence.
#
# Prerequisites: az CLI logged in (az login), an Azure subscription with the
# Container Apps provider registered, and a GitHub PAT with read:packages for
# pulling the image from GHCR.
#
# Required environment:
#   RESOURCE_GROUP     e.g. rg-scaleco
#   APP_NAME           e.g. scaleco
#   LOCATION           e.g. westeurope
#   CONTAINERAPP_ENV   Container Apps environment name, e.g. cae-scaleco
#   IMAGE              e.g. ghcr.io/owner/repo:sha
#   GHCR_USER          GitHub username or org that owns the package
#   GHCR_TOKEN         GitHub PAT with read:packages (also used by the app to pull)
#   DATABASE_URL       PostgreSQL DSN (managed Postgres, sslmode=require)
#   JWT_SECRET         >= 32 random bytes, same value used by the migrate job
#
# Re-running the script updates the app template (probes, scale, secrets) and
# leaves traffic where it is, so it is safe after an image rollout.
set -euo pipefail

for var in RESOURCE_GROUP APP_NAME LOCATION CONTAINERAPP_ENV IMAGE \
           GHCR_USER GHCR_TOKEN DATABASE_URL JWT_SECRET; do
  if [ -z "${!var:-}" ]; then
    echo "error: $var is required" >&2
    exit 1
  fi
done

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
TEMPLATE="$SCRIPT_DIR/containerapp.yaml"

echo "==> ensuring resource group $RESOURCE_GROUP in $LOCATION"
az group create --name "$RESOURCE_GROUP" --location "$LOCATION" --output none

if az containerapp env show --name "$CONTAINERAPP_ENV" --resource-group "$RESOURCE_GROUP" >/dev/null 2>&1; then
  echo "==> container apps environment $CONTAINERAPP_ENV already exists"
else
  echo "==> creating container apps environment $CONTAINERAPP_ENV (this can take a few minutes)"
  az containerapp env create \
    --name "$CONTAINERAPP_ENV" \
    --resource-group "$RESOURCE_GROUP" \
    --location "$LOCATION" \
    --logs-destination none \
    --output none
fi

ENV_ID=$(az containerapp env show \
  --name "$CONTAINERAPP_ENV" --resource-group "$RESOURCE_GROUP" \
  --query id --output tsv)
export ENV_ID

RENDERED=$(mktemp)
trap 'rm -f "$RENDERED"' EXIT
python3 - "$TEMPLATE" "$RENDERED" <<'PY'
import os
import sys

placeholders = (
    "APP_NAME", "LOCATION", "ENV_ID", "IMAGE",
    "JWT_SECRET", "DATABASE_URL", "GHCR_USER", "GHCR_TOKEN",
)
with open(sys.argv[1], "r", encoding="utf-8") as handle:
    rendered = handle.read()
for key in placeholders:
    rendered = rendered.replace(f"__{key}__", os.environ[key])
with open(sys.argv[2], "w", encoding="utf-8") as handle:
    handle.write(rendered)
PY

if az containerapp show --name "$APP_NAME" --resource-group "$RESOURCE_GROUP" >/dev/null 2>&1; then
  echo "==> updating container app $APP_NAME from template"
  az containerapp update \
    --name "$APP_NAME" \
    --resource-group "$RESOURCE_GROUP" \
    --yaml "$RENDERED" \
    --output none
else
  echo "==> creating container app $APP_NAME from template"
  az containerapp create \
    --resource-group "$RESOURCE_GROUP" \
    --yaml "$RENDERED" \
    --output none
fi

LATEST=$(az containerapp revision list \
  --name "$APP_NAME" --resource-group "$RESOURCE_GROUP" \
  --query "sort_by(@, &properties.createdTime)[-1].name" --output tsv)
if [ -n "$LATEST" ]; then
  echo "==> pinning 100% of traffic to $LATEST"
  az containerapp ingress traffic set \
    --name "$APP_NAME" \
    --resource-group "$RESOURCE_GROUP" \
    --revision-weight "$LATEST=100" \
    --output none
fi

FQDN=$(az containerapp show \
  --name "$APP_NAME" --resource-group "$RESOURCE_GROUP" \
  --query properties.configuration.ingress.fqdn --output tsv)
echo "==> done. endpoint: https://$FQDN"
echo "    check:    curl -fsS https://$FQDN/healthz && curl -fsS https://$FQDN/readyz"
echo "    roll out: push to main (CI runs deploy.yml) or see DEPLOYMENT.md"
