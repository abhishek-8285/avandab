#!/usr/bin/env bash
# ==============================================================================
# Cloudflare Terminal CLI Controller (scripts/cf.sh)
#
# Provides 100% terminal control over Cloudflare without using the browser UI.
#
# Supported commands:
#   ./scripts/cf.sh dns list                      - List all DNS records
#   ./scripts/cf.sh dns add <TYPE> <NAME> <VAL>   - Add record (e.g. A test 1.2.3.4)
#   ./scripts/cf.sh dns update <NAME> <VAL>       - Update record IP/value
#   ./scripts/cf.sh dns delete <NAME>             - Delete record by name
#   ./scripts/cf.sh cache purge                   - Purge entire Cloudflare cache
#   ./scripts/cf.sh dev [on|off]                  - Toggle Development Mode
#   ./scripts/cf.sh security [level]              - Get or set security level
#   ./scripts/cf.sh tunnel list                   - List active Cloudflare tunnels
#   ./scripts/cf.sh tunnel route <TUNNEL> <HOST>  - Bind hostname to tunnel
# ==============================================================================

set -euo pipefail

ZONE_NAME="${CF_ZONE_NAME:-avandab.com}"
ZONE_ID="${CF_ZONE_ID:-de3f6547a746385bdd674fee0b11fe3a}"
API_BASE="https://api.cloudflare.com/client/v4"

# Resolve API Token:
# 1. Environment variable CLOUDFLARE_API_TOKEN
# 2. ~/.cloudflare/token file
# 3. Extract token from ~/.cloudflared/cert.pem fallback
if [[ -n "${CLOUDFLARE_API_TOKEN:-}" ]]; then
    TOKEN="${CLOUDFLARE_API_TOKEN}"
elif [[ -f "${HOME}/.cloudflare/token" ]]; then
    TOKEN="$(cat "${HOME}/.cloudflare/token" | tr -d '[:space:]')"
elif [[ -f "${HOME}/.cloudflared/cert.pem" ]]; then
    TOKEN="$(grep -v -- '-----' "${HOME}/.cloudflared/cert.pem" | tr -d '[:space:]' | base64 -d 2>/dev/null | jq -r '.apiToken // empty' 2>/dev/null || true)"
elif [[ -f "${HOME}/.config/.wrangler/config/default.toml" ]]; then
    TOKEN="$(grep 'oauth_token' "${HOME}/.config/.wrangler/config/default.toml" | head -n1 | cut -d'"' -f2)"
else
    TOKEN=""
fi

cf_api() {
    local method="$1"
    local endpoint="$2"
    shift 2
    local data="${1:-}"

    if [[ -z "${TOKEN}" ]]; then
        echo "Error: No Cloudflare API Token found." >&2
        echo "Export CLOUDFLARE_API_TOKEN or save it to ~/.cloudflare/token" >&2
        exit 1
    fi

    local curl_args=(
        -s
        -X "${method}"
        -H "Authorization: Bearer ${TOKEN}"
        -H "Content-Type: application/json"
    )

    if [[ -n "${data}" ]]; then
        curl_args+=(-d "${data}")
    fi

    curl "${curl_args[@]}" "${API_BASE}${endpoint}"
}

cmd_dns() {
    local sub="${1:-list}"
    shift || true

    case "${sub}" in
        list)
            echo "Fetching DNS records for ${ZONE_NAME}..."
            cf_api GET "/zones/${ZONE_ID}/dns_records" | jq -r '
                if .success then
                    (["TYPE", "NAME", "CONTENT", "PROXIED", "ID"] | @tsv),
                    (.result[] | [.type, .name, .content, (.proxied|tostring), .id] | @tsv)
                else
                    "Error: " + (.errors[0].message // "Failed")
                end' | column -t
            ;;
        add)
            local type="${1:?Usage: cf.sh dns add <TYPE> <NAME> <CONTENT> [proxied=true|false]}"
            local name="${2:?Missing record name}"
            local content="${3:?Missing record content}"
            local proxied="${4:-true}"

            local payload
            payload=$(jq -n \
                --arg type "${type}" \
                --arg name "${name}" \
                --arg content "${content}" \
                --argjson proxied "${proxied}" \
                '{type: $type, name: $name, content: $content, proxied: $proxied, ttl: 1}')

            echo "Adding DNS record ${type} ${name} -> ${content} (proxied: ${proxied})..."
            cf_api POST "/zones/${ZONE_ID}/dns_records" "${payload}" | jq '{success, result: {id: .result.id, name: .result.name, content: .result.content}}'
            ;;
        update)
            local name="${1:?Usage: cf.sh dns update <NAME> <NEW_CONTENT>}"
            local content="${2:?Missing new content}"

            local rec_id
            rec_id=$(cf_api GET "/zones/${ZONE_ID}/dns_records?name=${name}" | jq -r '.result[0].id // empty')
            if [[ -z "${rec_id}" ]]; then
                echo "Error: DNS record for ${name} not found" >&2
                exit 1
            fi

            local cur_type
            cur_type=$(cf_api GET "/zones/${ZONE_ID}/dns_records/${rec_id}" | jq -r '.result.type')

            local payload
            payload=$(jq -n --arg type "${cur_type}" --arg name "${name}" --arg content "${content}" '{type: $type, name: $name, content: $content, proxied: true}')
            echo "Updating ${name} (${cur_type}) to ${content}..."
            cf_api PATCH "/zones/${ZONE_ID}/dns_records/${rec_id}" "${payload}" | jq '{success, result: {name: .result.name, content: .result.content}}'
            ;;
        delete)
            local name="${1:?Usage: cf.sh dns delete <NAME>}"
            local rec_id
            rec_id=$(cf_api GET "/zones/${ZONE_ID}/dns_records?name=${name}" | jq -r '.result[0].id // empty')
            if [[ -z "${rec_id}" ]]; then
                echo "Error: DNS record for ${name} not found" >&2
                exit 1
            fi

            echo "Deleting DNS record ${name} (ID: ${rec_id})..."
            cf_api DELETE "/zones/${ZONE_ID}/dns_records/${rec_id}" | jq '{success, errors}'
            ;;
        *)
            echo "Unknown dns command: ${sub}. Supported: list, add, update, delete" >&2
            exit 1
            ;;
    esac
}

cmd_cache() {
    local sub="${1:-rules}"
    shift || true

    case "${sub}" in
        rules)
            echo "Fetching active Cache Rules for ${ZONE_NAME}..."
            cf_api GET "/zones/${ZONE_ID}/rulesets/phases/http_request_cache_settings/entrypoint" | jq -r '
                if .success then
                    (["DESCRIPTION", "ENABLED", "ACTION"] | @tsv),
                    (.result.rules[] | [.description, (.enabled|tostring), .action] | @tsv)
                else
                    "Error: " + (.errors[0].message // "Failed")
                end' | column -t
            ;;
        purge)
            echo "Purging all cache for ${ZONE_NAME}..."
            cf_api POST "/zones/${ZONE_ID}/purge_cache" '{"purge_everything":true}' | jq '{success, errors}'
            ;;
        *)
            echo "Unknown cache command: ${sub}. Supported: rules, purge" >&2
            exit 1
            ;;
    esac
}

cmd_dev() {
    local val="${1:-}"
    if [[ -z "${val}" ]]; then
        cf_api GET "/zones/${ZONE_ID}/settings/development_mode" | jq '{status: .result.value, time_remaining: .result.time_remaining}'
    else
        cf_api PATCH "/zones/${ZONE_ID}/settings/development_mode" "$(jq -n --arg val "${val}" '{value: $val}')" | jq '{success, result: .result.value}'
    fi
}

cmd_security() {
    local val="${1:-}"
    if [[ -z "${val}" ]]; then
        cf_api GET "/zones/${ZONE_ID}/settings/security_level" | jq '{security_level: .result.value}'
    else
        cf_api PATCH "/zones/${ZONE_ID}/settings/security_level" "$(jq -n --arg val "${val}" '{value: $val}')" | jq '{success, security_level: .result.value}'
    fi
}

cmd_tunnel() {
    local sub="${1:-list}"
    shift || true

    case "${sub}" in
        list)
            cloudflared tunnel list
            ;;
        route)
            local tunnel="${1:?Usage: cf.sh tunnel route <TUNNEL_NAME_OR_ID> <HOSTNAME>}"
            local host="${2:?Missing hostname}"
            cloudflared tunnel route dns "${tunnel}" "${host}"
            ;;
        *)
            cloudflared tunnel "${sub}" "$@"
            ;;
    esac
}

case "${1:-help}" in
    dns)
        shift
        cmd_dns "$@"
        ;;
    cache)
        shift
        cmd_cache "$@"
        ;;
    dev)
        shift
        cmd_dev "$@"
        ;;
    security)
        shift
        cmd_security "$@"
        ;;
    tunnel)
        shift
        cmd_tunnel "$@"
        ;;
    help|--help|-h)
        echo "Cloudflare CLI Controller for ${ZONE_NAME}"
        echo
        echo "Usage: ./scripts/cf.sh <command> [options]"
        echo
        echo "Commands:"
        echo "  dns list                               - List all DNS records"
        echo "  dns add <TYPE> <NAME> <VALUE> [proxy]  - Add DNS record"
        echo "  dns update <NAME> <VALUE>              - Update record value"
        echo "  dns delete <NAME>                      - Delete DNS record"
        echo "  cache purge                            - Purge entire edge cache"
        echo "  dev [on|off]                           - Get or toggle Development Mode"
        echo "  security [off|low|medium|high|under_attack] - Security level"
        echo "  tunnel list                            - List Cloudflare tunnels"
        echo "  tunnel route <TUNNEL> <HOSTNAME>       - Route hostname to tunnel"
        ;;
    *)
        echo "Unknown command: $1. Run './scripts/cf.sh help' for usage." >&2
        exit 1
        ;;
esac
