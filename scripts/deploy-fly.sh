#!/bin/sh
set -eu

repo_url="https://github.com/Thiht/pici.git"
script_url="https://raw.githubusercontent.com/Thiht/pici/main/scripts/deploy-fly.sh"
default_ref="main"

usage() {
  cat <<'EOF'
Deploy pici on Fly.io.

Usage:
  deploy-fly.sh [options]

Options:
  --name NAME        Fly app name (default: generated)
  --region REGION    Fly region (default: cdg)
  --org ORG          Fly organization (default: your default organization)
  --provider NAME    Auth provider: github, gitlab or oidc (default: github)
  --ref REF          Git ref to deploy when not run from the pici repo (default: main)
  --no-depot         Build with Fly's legacy remote builder instead of Depot
  --dry-run          Print the commands instead of running them
  -h, --help         Show this help

Environment:
  PICI_FLY_APP, PICI_FLY_REGION, PICI_FLY_ORG, PICI_FLY_REF
  PICI_PUBLIC_URL, PICI_SECRET_KEY, PICI_AUTH_PROVIDER
  PICI_GITHUB_CLIENT_ID, PICI_GITHUB_CLIENT_SECRET
  PICI_GITHUB_TEAMS, PICI_GITHUB_USERS
  PICI_GITLAB_URL, PICI_GITLAB_CLIENT_ID, PICI_GITLAB_CLIENT_SECRET
  PICI_GITLAB_GROUPS, PICI_GITLAB_USERS
  PICI_OIDC_ISSUER, PICI_OIDC_CLIENT_ID, PICI_OIDC_CLIENT_SECRET
  PICI_OIDC_GROUPS, PICI_OIDC_USERS, PICI_OIDC_SCOPES, PICI_OIDC_NAME

The script is interactive unless every required value is provided in the
environment. It prints the sign-in callback URL and asks for the application
credentials.

Examples:
  ./scripts/deploy-fly.sh
  ./scripts/deploy-fly.sh --name my-pici --region cdg
  PICI_FLY_APP=my-pici PICI_GITHUB_CLIENT_ID=... \
    PICI_GITHUB_CLIENT_SECRET=... PICI_GITHUB_USERS=me \
    ./scripts/deploy-fly.sh
EOF
}

die() {
  echo "error: $*" >&2
  exit 1
}

need_value() {
  [ "$#" -ge 2 ] || die "missing value for $1"
}

app=${PICI_FLY_APP:-}
region=${PICI_FLY_REGION:-}
org=${PICI_FLY_ORG:-}
provider=${PICI_AUTH_PROVIDER:-}
ref=${PICI_FLY_REF:-$default_ref}
dry_run=0
no_depot=0
local_repo=0
app_ready=0
secrets_staged=0
deploy_started=0
deploy_done=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --name)
      need_value "$@"
      app=$2
      shift 2
      ;;
    --name=*)
      app=${1#*=}
      shift
      ;;
    --region)
      need_value "$@"
      region=$2
      shift 2
      ;;
    --region=*)
      region=${1#*=}
      shift
      ;;
    --org)
      need_value "$@"
      org=$2
      shift 2
      ;;
    --org=*)
      org=${1#*=}
      shift
      ;;
    --provider)
      need_value "$@"
      provider=$2
      shift 2
      ;;
    --provider=*)
      provider=${1#*=}
      shift
      ;;
    --ref)
      need_value "$@"
      ref=$2
      shift 2
      ;;
    --ref=*)
      ref=${1#*=}
      shift
      ;;
    --no-depot)
      no_depot=1
      shift
      ;;
    --dry-run)
      dry_run=1
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      die "unknown option: $1"
      ;;
  esac
done

if [ -z "$ref" ]; then
  ref=$default_ref
fi

if [ "$dry_run" -eq 0 ]; then
  command -v fly >/dev/null 2>&1 || die "flyctl is required: https://fly.io/docs/flyctl/install/"
  fly auth whoami >/dev/null 2>&1 || die "not logged in to Fly; run 'fly auth signup' or 'fly auth login'"
fi
command -v openssl >/dev/null 2>&1 || die "openssl is required"

tmp_repo=
secrets_file=
tty=
if [ -t 0 ]; then
  tty=/dev/tty
fi

failure_help() {
  if [ "$app_ready" -eq 0 ]; then
    cat >&2 <<'EOF'

Deployment did not complete. Fix the error above and rerun the same command.
EOF
    return 0
  fi

  if [ "$local_repo" -eq 1 ] && [ -f "$0" ]; then
    rerun="$0"
  else
    rerun="sh -c \"\$(curl -fsSL $script_url)\" deploy-fly"
  fi
  rerun="$rerun --name $app --region $region --provider $provider"
  if [ -n "$org" ]; then
    rerun="$rerun --org $org"
  fi
  if [ "$local_repo" -eq 0 ]; then
    rerun="$rerun --ref $ref"
  fi
  if [ "$no_depot" -eq 1 ]; then
    rerun="$rerun --no-depot"
  fi
  if [ "$public_url" != "https://$app.fly.dev" ]; then
    rerun="PICI_PUBLIC_URL=$public_url $rerun"
  fi

  {
    printf '\n%s\n' '----------------------------------------------------------------------'
    printf '%s\n' 'Deployment did not complete.'
    printf '\n'
    if [ "$secrets_staged" -eq 1 ]; then
      printf '%s\n' 'The app and its staged secrets are kept. Rerunning is safe and idempotent:'
      printf '%s\n' 'the existing app is reused, PICI_SECRET_KEY is kept, and the other secrets'
      printf '%s\n' 'are replaced. Fly does not expose secret values, so you will be asked for'
      printf '%s\n' 'the sign-in credentials again.'
    else
      printf '%s\n' 'Rerunning is safe and idempotent: the existing app, if any, is reused and'
      printf '%s\n' 'PICI_SECRET_KEY is kept.'
    fi
    printf '\n%s\n' 'Rerun:'
    printf '  %s\n' "$rerun"
    if [ "$deploy_started" -eq 1 ] && [ "$no_depot" -eq 0 ]; then
      printf '\n%s\n' 'If the build failed in Depot (timeout, connection reset), add --no-depot:'
      printf '  %s --no-depot\n' "$rerun"
    fi
    if [ "$secrets_staged" -eq 1 ] && [ "$local_repo" -eq 1 ] && [ -d "$repo" ]; then
      printf '\n%s\n' 'Or deploy directly with the secrets already staged:'
      printf '  fly deploy . -a %s --primary-region %s --ha=false --now --depot=false\n' "$app" "$region"
    fi
    if [ "$deploy_started" -eq 1 ] || [ "$secrets_staged" -eq 1 ]; then
      printf '\n%s\n' 'Then check the deployment:'
      printf '  fly status -a %s\n' "$app"
      printf '  fly logs -a %s\n' "$app"
    fi
    printf '%s\n' '----------------------------------------------------------------------'
  } >&2
  return 0
}

cleanup() {
  status=$?
  if [ -n "$tty" ]; then
    stty echo <"$tty" 2>/dev/null || true
  fi
  if [ -n "$tmp_repo" ]; then
    rm -rf "$tmp_repo" || true
  fi
  if [ -n "$secrets_file" ]; then
    rm -f "$secrets_file" || true
  fi
  if [ "$status" -ne 0 ] && [ "$deploy_done" -eq 0 ]; then
    failure_help
  fi
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

run() {
  if [ "$dry_run" -eq 1 ]; then
    printf '+'
    for _arg in "$@"; do
      printf ' %s' "$_arg"
    done
    printf '\n'
    return 0
  fi
  "$@"
}

prompt() {
  _label=$1
  _default=${2:-}
  if [ -n "$_default" ]; then
    printf '%s [%s]: ' "$_label" "$_default" >&2
  else
    printf '%s: ' "$_label" >&2
  fi
  if [ -n "$tty" ]; then
    IFS= read -r _value <"$tty" || _value=
  else
    IFS= read -r _value || _value=
  fi
  if [ -z "$_value" ]; then
    _value=$_default
  fi
  printf '%s' "$_value"
}

prompt_secret() {
  _label=$1
  printf '%s: ' "$_label" >&2
  if [ -n "$tty" ]; then
    stty -echo <"$tty" 2>/dev/null || true
    IFS= read -r _value <"$tty" || _value=
    stty echo <"$tty" 2>/dev/null || true
    printf '\n' >&2
  else
    IFS= read -r _value || _value=
  fi
  printf '%s' "$_value"
}

if [ -z "$app" ]; then
  app=$(prompt "Fly app name" "pici-$(openssl rand -hex 2)")
fi
if ! printf '%s' "$app" | grep -Eq '^[a-z][a-z0-9-]{1,28}[a-z0-9]$'; then
  die "invalid Fly app name '$app': use 3-30 lowercase letters, digits and hyphens, starting with a letter and ending with a letter or digit"
fi

if [ -z "$region" ]; then
  region=$(prompt "Fly region" "cdg")
fi
if ! printf '%s' "$region" | grep -Eq '^[a-z]{3}$'; then
  die "invalid Fly region '$region': use a three-letter region code such as cdg, iad or syd"
fi

if [ -z "$provider" ]; then
  provider=$(prompt "Auth provider (github, gitlab or oidc)" "github")
fi
case "$provider" in
  github | gitlab | oidc) ;;
  *) die "invalid auth provider '$provider': use github, gitlab or oidc" ;;
esac

public_url=${PICI_PUBLIC_URL:-https://$app.fly.dev}
public_url=${public_url%/}
case "$public_url" in
  https://*) ;;
  *) die "PICI_PUBLIC_URL must use https: $public_url" ;;
esac
case "$public_url" in
  https://*/*) die "PICI_PUBLIC_URL must be an origin without a path: $public_url" ;;
esac
callback="$public_url/auth/$provider/callback"
app_ready=1

if [ -f fly.toml ] && [ -f docker/Dockerfile ] && [ -f cmd/pici/main.go ]; then
  repo=$(pwd)
  local_repo=1
  echo "Using pici source from $repo" >&2
else
  command -v git >/dev/null 2>&1 || die "git is required to fetch the pici source"
  tmp_repo=$(mktemp -d)
  repo=$tmp_repo
  run git clone --depth 1 --branch "$ref" "$repo_url" "$tmp_repo"
fi

app_created=0
if [ "$dry_run" -eq 0 ] && fly status --app "$app" >/dev/null 2>&1; then
  echo "Using existing Fly app $app" >&2
else
  if [ -n "$org" ]; then
    run fly apps create "$app" --org "$org"
  else
    run fly apps create "$app"
  fi
  app_created=1
fi

if [ "$provider" = "github" ]; then
  github_client_id=${PICI_GITHUB_CLIENT_ID:-}
  github_client_secret=${PICI_GITHUB_CLIENT_SECRET:-}
  github_teams=${PICI_GITHUB_TEAMS:-}
  github_users=${PICI_GITHUB_USERS:-}

  if [ -z "$github_client_id" ] || [ -z "$github_client_secret" ]; then
    cat >&2 <<EOF

Create a GitHub OAuth App:
  https://github.com/settings/applications/new

  Homepage URL:                $public_url
  Authorization callback URL:  $callback

EOF
    if [ -z "$github_client_id" ]; then
      github_client_id=$(prompt "GitHub OAuth client id")
    fi
    if [ -z "$github_client_secret" ]; then
      github_client_secret=$(prompt_secret "GitHub OAuth client secret")
    fi
  fi

  if [ -z "$github_users" ] && [ -z "$github_teams" ]; then
    github_users=$(prompt "Allowed GitHub logins (comma-separated)")
  fi
  if [ -z "$github_users" ] && [ -z "$github_teams" ]; then
    github_teams=$(prompt "Allowed GitHub teams (org/team, comma-separated)")
  fi
  if [ -z "$github_users" ] && [ -z "$github_teams" ]; then
    die "at least one GitHub user or team is required"
  fi
  [ -n "$github_client_id" ] || die "GitHub OAuth client id is required"
  [ -n "$github_client_secret" ] || die "GitHub OAuth client secret is required"
elif [ "$provider" = "gitlab" ]; then
  gitlab_url=${PICI_GITLAB_URL:-https://gitlab.com}
  gitlab_url=${gitlab_url%/}
  gitlab_client_id=${PICI_GITLAB_CLIENT_ID:-}
  gitlab_client_secret=${PICI_GITLAB_CLIENT_SECRET:-}
  gitlab_groups=${PICI_GITLAB_GROUPS:-}
  gitlab_users=${PICI_GITLAB_USERS:-}

  if [ -z "$gitlab_client_id" ] || [ -z "$gitlab_client_secret" ]; then
    cat >&2 <<EOF

Create a GitLab OAuth application:
  $gitlab_url/-/user_settings/applications

  Redirect URI:  $callback
  Scopes:        read_api, offline_access

EOF
    if [ -z "$gitlab_client_id" ]; then
      gitlab_client_id=$(prompt "GitLab OAuth application id")
    fi
    if [ -z "$gitlab_client_secret" ]; then
      gitlab_client_secret=$(prompt_secret "GitLab OAuth application secret")
    fi
  fi

  if [ -z "$gitlab_groups" ] && [ -z "$gitlab_users" ]; then
    gitlab_groups=$(prompt "Allowed GitLab groups (comma-separated)")
  fi
  if [ -z "$gitlab_groups" ] && [ -z "$gitlab_users" ]; then
    gitlab_users=$(prompt "Allowed GitLab usernames (comma-separated)")
  fi
  if [ -z "$gitlab_groups" ] && [ -z "$gitlab_users" ]; then
    die "at least one GitLab group or username is required"
  fi
  [ -n "$gitlab_client_id" ] || die "GitLab OAuth application id is required"
  [ -n "$gitlab_client_secret" ] || die "GitLab OAuth application secret is required"
else
  oidc_issuer=${PICI_OIDC_ISSUER:-}
  oidc_client_id=${PICI_OIDC_CLIENT_ID:-}
  oidc_client_secret=${PICI_OIDC_CLIENT_SECRET:-}
  oidc_groups=${PICI_OIDC_GROUPS:-}
  oidc_users=${PICI_OIDC_USERS:-}
  oidc_scopes=${PICI_OIDC_SCOPES:-}
  oidc_name=${PICI_OIDC_NAME:-}

  if [ -z "$oidc_issuer" ]; then
    oidc_issuer=$(prompt "OIDC issuer URL")
  fi
  if [ -z "$oidc_client_id" ] || [ -z "$oidc_client_secret" ]; then
    cat >&2 <<EOF

Create a confidential OIDC application in your provider:
  Redirect URI:  $callback
  Scopes:        ${oidc_scopes:-openid profile email offline_access}

EOF
    if [ -z "$oidc_client_id" ]; then
      oidc_client_id=$(prompt "OIDC client id")
    fi
    if [ -z "$oidc_client_secret" ]; then
      oidc_client_secret=$(prompt_secret "OIDC client secret")
    fi
  fi

  if [ -z "$oidc_groups" ] && [ -z "$oidc_users" ]; then
    oidc_groups=$(prompt "Allowed OIDC groups (comma-separated)")
  fi
  if [ -z "$oidc_groups" ] && [ -z "$oidc_users" ]; then
    oidc_users=$(prompt "Allowed OIDC usernames or emails (comma-separated)")
  fi
  if [ -z "$oidc_groups" ] && [ -z "$oidc_users" ]; then
    die "at least one OIDC group or user is required"
  fi
  [ -n "$oidc_issuer" ] || die "OIDC issuer URL is required"
  [ -n "$oidc_client_id" ] || die "OIDC client id is required"
  [ -n "$oidc_client_secret" ] || die "OIDC client secret is required"
fi

existing_secret_key=0
secret_names=
if [ "$dry_run" -eq 0 ] && [ "$app_created" -eq 0 ]; then
  secret_names=$(fly secrets list --app "$app" 2>/dev/null) || die "could not list the secrets of Fly app $app"
  case "$secret_names" in
    *PICI_SECRET_KEY*) existing_secret_key=1 ;;
  esac
fi

secret_key=
secret_key_generated=0
if [ "$existing_secret_key" -eq 1 ]; then
  echo "Keeping the existing PICI_SECRET_KEY (Fly does not expose it; unset it first to rotate)." >&2
else
  secret_key=${PICI_SECRET_KEY:-}
  if [ -z "$secret_key" ]; then
    secret_key=$(openssl rand -hex 32)
    secret_key_generated=1
  fi
fi
if [ "$secret_key_generated" -eq 1 ] && [ "$dry_run" -eq 0 ]; then
  cat <<EOF

Generated PICI_SECRET_KEY (save it for backups and restores):
  $secret_key

EOF
fi

secrets_file=$(mktemp)
chmod 600 "$secrets_file"
{
  if [ -n "$secret_key" ]; then
    printf 'PICI_SECRET_KEY=%s\n' "$secret_key"
  fi
  printf 'PICI_PUBLIC_URL=%s\n' "$public_url"
  printf 'PICI_AUTH_PROVIDER=%s\n' "$provider"
  if [ "$provider" = "github" ]; then
    printf 'PICI_GITHUB_CLIENT_ID=%s\n' "$github_client_id"
    printf 'PICI_GITHUB_CLIENT_SECRET=%s\n' "$github_client_secret"
    if [ -n "$github_users" ]; then
      printf 'PICI_GITHUB_USERS=%s\n' "$github_users"
    fi
    if [ -n "$github_teams" ]; then
      printf 'PICI_GITHUB_TEAMS=%s\n' "$github_teams"
    fi
  elif [ "$provider" = "gitlab" ]; then
    printf 'PICI_GITLAB_URL=%s\n' "$gitlab_url"
    printf 'PICI_GITLAB_CLIENT_ID=%s\n' "$gitlab_client_id"
    printf 'PICI_GITLAB_CLIENT_SECRET=%s\n' "$gitlab_client_secret"
    if [ -n "$gitlab_groups" ]; then
      printf 'PICI_GITLAB_GROUPS=%s\n' "$gitlab_groups"
    fi
    if [ -n "$gitlab_users" ]; then
      printf 'PICI_GITLAB_USERS=%s\n' "$gitlab_users"
    fi
  else
    printf 'PICI_OIDC_ISSUER=%s\n' "$oidc_issuer"
    printf 'PICI_OIDC_CLIENT_ID=%s\n' "$oidc_client_id"
    printf 'PICI_OIDC_CLIENT_SECRET=%s\n' "$oidc_client_secret"
    if [ -n "$oidc_groups" ]; then
      printf 'PICI_OIDC_GROUPS=%s\n' "$oidc_groups"
    fi
    if [ -n "$oidc_users" ]; then
      printf 'PICI_OIDC_USERS=%s\n' "$oidc_users"
    fi
    if [ -n "$oidc_scopes" ]; then
      printf 'PICI_OIDC_SCOPES=%s\n' "$oidc_scopes"
    fi
    if [ -n "$oidc_name" ]; then
      printf 'PICI_OIDC_NAME=%s\n' "$oidc_name"
    fi
  fi
} >"$secrets_file"

if [ "$dry_run" -eq 1 ]; then
  echo "Secrets that would be set:" >&2
  sed 's/=.*/=<redacted>/' "$secrets_file" >&2
else
  fly secrets import --app "$app" --stage <"$secrets_file"
  secrets_staged=1
fi

unset_stale_secret() {
  if [ -z "$2" ]; then
    case "$secret_names" in
      *"$1"*) run fly secrets unset --app "$app" --stage "$1" ;;
    esac
  fi
}
if [ "$provider" = "github" ]; then
  unset_stale_secret PICI_GITHUB_USERS "$github_users"
  unset_stale_secret PICI_GITHUB_TEAMS "$github_teams"
elif [ "$provider" = "gitlab" ]; then
  unset_stale_secret PICI_GITLAB_GROUPS "$gitlab_groups"
  unset_stale_secret PICI_GITLAB_USERS "$gitlab_users"
else
  unset_stale_secret PICI_OIDC_GROUPS "$oidc_groups"
  unset_stale_secret PICI_OIDC_USERS "$oidc_users"
fi

deploy_started=1
if [ "$no_depot" -eq 1 ]; then
  run fly deploy "$repo" --app "$app" --primary-region "$region" --ha=false --now --depot=false
else
  run fly deploy "$repo" --app "$app" --primary-region "$region" --ha=false --now
fi
deploy_done=1

if [ "$dry_run" -eq 1 ]; then
  echo "Dry run complete; no Fly resources were changed." >&2
else
  cat <<EOF

pici is deployed.

  URL:      $public_url
  Callback: $callback

Useful commands:
  fly logs -a $app
  fly status -a $app
  fly ssh console -a $app
EOF
fi
