#!/bin/bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="${ROOT_DIR}/bin"
PUBLISH_DIR="${BIN_DIR}/publish"
VERSION="${BUILD_VERSION:?BUILD_VERSION is required}"
COMMIT="${GIT_COMMIT:?GIT_COMMIT is required}"
BASE_URL="${ARTIFACT_BASE_URL:?ARTIFACT_BASE_URL is required}"
PUBLISHED_AT="$(date -u -d "@${SOURCE_DATE_EPOCH:?SOURCE_DATE_EPOCH is required}" +%Y-%m-%dT%H:%M:%SZ)"

rm -rf "${PUBLISH_DIR}"
mkdir -p "${PUBLISH_DIR}/updater/releases/agent" "${PUBLISH_DIR}/updater/releases/endpoint"

artifact_json() {
    local component="$1" arch="$2" filename="$3" source="$4" role="app"
    local target="${PUBLISH_DIR}/${component}/artifacts/linux/${arch}/${VERSION}/${filename}"
    local sha
    mkdir -p "$(dirname "${target}")"
    cp "${source}" "${target}"
    sha="$(sha256sum "${source}" | awk '{print $1}')"
    jq -n --arg arch "${arch}" --arg filename "${filename}" \
        --arg url "${BASE_URL}/${component}/artifacts/linux/${arch}/${VERSION}/${filename}" \
        --arg sha "${sha}" \
        --argjson size "$(stat -c %s "${source}")" \
        '{os:"linux",arch:$arch,role:"app",package_type:"binary",filename:$filename,download_url:$url,size:$size,sha256:$sha}'
}

write_manifest() {
    local component="$1" filename="$2" artifacts='[]' arch source item
    for arch in amd64 arm64; do
        source="${BIN_DIR}/signal_${component}-linux-${arch}"
        test -f "${source}"
        item="$(artifact_json "${component}" "${arch}" "${filename}" "${source}")"
        artifacts="$(jq -c --argjson item "${item}" '. + [$item]' <<< "${artifacts}")"
    done
    jq -n --arg published_at "${PUBLISHED_AT}" --arg commit_date "${PUBLISHED_AT}" --arg component "${component}" \
        --arg version "${VERSION}" --arg commit "${COMMIT}" --argjson artifacts "${artifacts}" \
        '{schema_version:1,published_at:$published_at,release:{component:$component,version:$version,commit_id:$commit,commit_date:$commit_date,channel:"stable",release_notes:(($component|ascii_upcase)+" build "+$commit),min_supported_version:""},artifacts:$artifacts}' \
        > "${PUBLISH_DIR}/updater/releases/${component}/latest.json"
}

write_manifest agent signal_agent
write_manifest endpoint signal_endpoint

cp "${ROOT_DIR}/scripts/install_agent.sh" "${PUBLISH_DIR}/install_agent.sh"
cp "${ROOT_DIR}/scripts/install_signal.sh" "${PUBLISH_DIR}/install_signal.sh"
cp "${ROOT_DIR}/scripts/install_endpoint.sh" "${PUBLISH_DIR}/install_endpoint.sh"
cp "${BIN_DIR}/signal_endpoint-linux-amd64" "${PUBLISH_DIR}/signal_endpoint-${VERSION}-linux-amd64"
cp "${BIN_DIR}/signal_endpoint-linux-arm64" "${PUBLISH_DIR}/signal_endpoint-${VERSION}-linux-arm64"

jq -n --arg generated_at "${PUBLISHED_AT}" --arg base "${BASE_URL}" \
    '{schema_version:1,generated_at:$generated_at,manifests:[$base+"/updater/releases/agent/latest.json",$base+"/updater/releases/endpoint/latest.json",$base+"/updater/releases/desktop/latest.json"]}' \
    > "${PUBLISH_DIR}/updater/catalog.json"

for component in agent endpoint; do
    jq '{version:.release.version,commit_id:.release.commit_id,build_date:.published_at,files:(.artifacts|map({key:(.os+"-"+.arch),value:.download_url})|from_entries),sha256:(.artifacts|map({key:(.os+"-"+.arch),value:.sha256})|from_entries)}' \
        "${PUBLISH_DIR}/updater/releases/${component}/latest.json" > "${PUBLISH_DIR}/signal_${component}-version.json"
done
