#!/usr/bin/env bash
set -e

YQ_VERSION="${YQ_VERSION:-4.52.5}"

if command -v yq >/dev/null 2>&1; then
	echo "yq already installed: $(yq --version)"
	exit 0
fi

case "$(uname -s)" in
	Darwin)
		if command -v brew >/dev/null 2>&1; then
			echo "Installing yq via Homebrew..."
			brew install yq
		else
			echo "Error: Homebrew not found. Install from https://brew.sh"
			exit 1
		fi
		;;
	Linux)
		echo "Installing yq from GitHub release..."
		arch=$(uname -m)
		case "$arch" in
			x86_64) arch=amd64 ;;
			aarch64|arm64) arch=arm64 ;;
			*) echo "Unsupported arch: $arch"; exit 1 ;;
		esac
		url="https://github.com/mikefarah/yq/releases/download/v${YQ_VERSION}/yq_linux_${arch}"
		bin_dir="/usr/local/bin"
		sudo curl -sSL "$url" -o "$bin_dir/yq" && sudo chmod +x "$bin_dir/yq"
		echo "Installed mikefarah/yq v${YQ_VERSION} to $bin_dir/yq"
		;;
	*)
		echo "Unsupported OS: $(uname -s)"
		exit 1
		;;
esac
