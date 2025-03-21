#!/bin/sh

set -eu

if [ -z "${APP_VERSION:-}" ]; then
  # `select(.tag_name | test("-") | not)` should filter out any prerelease versions which contain hyphens (eg. 1.19.0-beta1)
  # `select(.tag_name | startswith("v1."))` will filter out any "v2". Though we might be able to get rid of this (see https://github.com/kgateway-dev/kgateway/pull/8856)
  APP_VERSIONS=$(curl -sHL "Accept: application/vnd.github.v3+json" https://api.github.com/repos/nmnellis/mesh-helper/releases | jq -r '[.[] | select(.tag_name | test("-") | not) | select(.tag_name | startswith("v")) | .tag_name] | sort_by(.) | reverse | .[0]')
else
  APP_VERSIONS="${APP_VERSION}"
fi

if [ "$(uname -s)" = "Darwin" ]; then
  OS=darwin
else
  OS=linux
fi

arch=$(uname -m)
if [ "$arch" = "aarch64" ] || [ "$arch" = "arm64" ]; then
  GOARCH=arm64
else
  GOARCH=amd64
fi

for version in $APP_VERSIONS; do

tmp=$(mktemp -d /tmp/gloo.XXXXXX)
filename="mesh_helper_${OS}_${GOARCH}"

url="https://github.com/nmnellis/mesh-helper/releases/download/${version}/${filename}.zip"
if curl -f ${url} >/dev/null 2>&1; then
  echo "Attempting to download mesh-helper version ${version}"
else
  continue
fi

(
  cd "$tmp"

  echo "Downloading ${filename}..."

  SHA=$(curl -sL "${url}.sha256" | cut -d' ' -f1)
  curl -sLO "${url}"
  echo "Download complete!"
)

(
  cd "$HOME"
  mkdir -p ".istio/bin"
  echo "extracting zip ${tmp}/${filename}"
  unzip "${tmp}/${filename}.zip" -d ${tmp}
  mv "${tmp}/mesh-helper" ".istio/bin/mesh-helper"
  chmod +x ".istio/bin/mesh-helper"
)

rm -r "$tmp"

echo "mesh-helper was successfully installed 🎉"
echo ""
echo "Add the CLI to your path with:"
echo "  export PATH=\$HOME/.istio/bin:\$PATH"
echo ""
echo "Now run:"
echo "  $HOME/.istio/bin/mesh-helper -h"
exit 0
done

echo "No versions of mesh-helper found."
exit 1