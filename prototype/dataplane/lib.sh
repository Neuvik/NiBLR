# Shared helpers for the NiBLR data-plane proof. Must run as root.
set -euo pipefail
NS_CLIENT=niblr-client; NS_HUB=niblr-hub; NS_NET=niblr-net
NS_EXITS=(niblr-e1 niblr-e2)          # exit i -> mark i (1-based)
KEYDIR=${KEYDIR:-/tmp/niblr-proto-keys}
nsx() { local ns=$1; shift; ip netns exec "$ns" "$@"; }
