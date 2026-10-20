#!/usr/bin/env bash
set -euo pipefail

NUDENET_SOURCE_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
NUDENET_INSTALL_DIR=/opt/lyrarma/nudenet

as_root() {
    if [[ "$EUID" -eq 0 ]]; then
        "$@"
    else
        sudo "$@"
    fi
}

main() {
    if [[ "${1:-}" == --help || "${1:-}" == -h ]]; then
        printf 'Usage: bash tools/nudenet/install.sh\n\nInstall the local NudeNet moderation adapter on Debian/Ubuntu\nNo application configuration is changed\n'
        return
    fi
    [[ "$#" -eq 0 ]] || { printf 'ERROR: Unknown option\n' >&2; exit 1; }
    [[ -r /etc/os-release ]] || { printf 'ERROR: Cannot determine the operating system\n' >&2; exit 1; }
    . /etc/os-release
    case "${ID:-}" in
        debian|ubuntu) ;;
        *) printf 'ERROR: This installer supports Debian and Ubuntu only\n' >&2; exit 1 ;;
    esac
    if [[ "$EUID" -ne 0 ]]; then
        command -v sudo >/dev/null 2>&1 || { printf 'ERROR: Install sudo or run as root\n' >&2; exit 1; }
        sudo -v
    fi
    as_root apt-get update
    as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y python3 python3-venv ca-certificates
    as_root install -d -m 755 "$NUDENET_INSTALL_DIR"
    as_root python3 -m venv "$NUDENET_INSTALL_DIR/venv"
    as_root "$NUDENET_INSTALL_DIR/venv/bin/python" -m pip install --disable-pip-version-check --requirement "$NUDENET_SOURCE_DIR/requirements.txt"
    as_root install -m 644 "$NUDENET_SOURCE_DIR/moderate.py" "$NUDENET_INSTALL_DIR/moderate.py"
    if ! as_root timeout 60s "$NUDENET_INSTALL_DIR/venv/bin/python" "$NUDENET_INSTALL_DIR/moderate.py" --self-test; then
        printf 'ERROR: NudeNet model self-test failed; check the dependency output and available memory\n' >&2
        exit 1
    fi
    printf '\nNudeNet is ready. Configure the server and restart it:\nSCANNER_MODERATION_COMMAND="%s/venv/bin/python %s/moderate.py"\n' "$NUDENET_INSTALL_DIR" "$NUDENET_INSTALL_DIR"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi
