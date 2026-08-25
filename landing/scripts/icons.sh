#!/usr/bin/env bash
# Generates every icon the landing and the documentation reference, from one
# square source image, into landing/public/.
#
# This is a maintenance script, not a build step. The icons are committed, so CI
# never runs this: CI is Linux and the fallback tool here is macOS-only. Run it
# when the mark changes, then commit what it wrote.
#
# The mark is a raster, so there is no generated SVG. favicon.svg stays whatever
# is committed; pass --drop-svg to retire it when the raster mark replaces it.
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
usage: landing/scripts/icons.sh SOURCE [--bg HEX] [--zoom PERCENT] [--drop-svg]

  SOURCE          square PNG or JPEG, at least 512x512; 1024 or larger is better
  --bg HEX        colour of the canvas a padded output is composited on, without
                  the leading '#'. Default 0a0b0d, the page colour. If the source
                  carries its own background, pass THAT colour: padding with a
                  different one leaves a visible box inside the image.
  --zoom PERCENT  crop the source to its central PERCENT before resizing, so a
                  mark that floats in a wide margin fills the tile. Default 100,
                  no crop. An app icon usually wants its mark at about 80% of the
                  tile: if the mark measures 60% of the source, pass 76.
  --drop-svg      remove landing/public/favicon.svg

writes into landing/public/:
  favicon-16.png  favicon-32.png  apple-touch-icon.png (180)
  icon-192.png  icon-512.png  icon-maskable-512.png

it does NOT write og.png. The social card carries type, which ImageMagick here
cannot set: it is rendered from landing/scripts/social-card.html instead.
EOF
  exit 2
}

[ $# -ge 1 ] || usage
source_image=$1
shift
background="0a0b0d"
zoom=100
drop_svg=false
while [ $# -gt 0 ]; do
  case $1 in
    --bg) [ $# -ge 2 ] || usage; background=${2#\#}; shift 2 ;;
    --zoom) [ $# -ge 2 ] || usage; zoom=$2; shift 2 ;;
    --drop-svg) drop_svg=true; shift ;;
    *) usage ;;
  esac
done
[ -f "$source_image" ] || { echo "no such file: $source_image" >&2; exit 1; }
case $background in
  [0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F][0-9a-fA-F]) ;;
  *) echo "--bg wants six hex digits, got: $background" >&2; exit 1 ;;
esac
case $zoom in
  ''|*[!0-9]*) echo "--zoom wants a whole percent, got: $zoom" >&2; exit 1 ;;
  *) [ "$zoom" -ge 10 ] && [ "$zoom" -le 100 ] ||
    { echo "--zoom out of range 10..100: $zoom" >&2; exit 1; } ;;
esac

here=$(cd "$(dirname "$0")/.." && pwd)
out="$here/public"

# ImageMagick first because it composites in one pass; sips is the macOS
# fallback and reaches the same result in two.
if command -v magick >/dev/null 2>&1; then
  tool=magick
elif command -v sips >/dev/null 2>&1; then
  tool=sips
else
  echo "need ImageMagick (magick) or macOS sips; found neither" >&2
  exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Every output starts from `framed`: the source, cropped to its central --zoom
# percent. Cropping once here rather than per size keeps every icon consistent
# and keeps the cropping arithmetic in one place.
framed="$work/framed.png"
if [ "$zoom" -eq 100 ]; then
  case $tool in
    magick) magick "$source_image" -strip "PNG24:$framed" ;;
    sips) sips --setProperty format png "$source_image" --out "$framed" >/dev/null ;;
  esac
else
  src_w=$(sips -g pixelWidth "$source_image" | awk '/pixelWidth/ {print $2}')
  src_h=$(sips -g pixelHeight "$source_image" | awk '/pixelHeight/ {print $2}')
  crop_w=$((src_w * zoom / 100))
  crop_h=$((src_h * zoom / 100))
  case $tool in
    magick)
      magick "$source_image" -gravity center -crop "${crop_w}x${crop_h}+0+0" \
        +repage -strip "PNG24:$framed"
      ;;
    sips)
      # --cropToHeightWidth crops about the centre, which is where a mark on a
      # square canvas already sits.
      sips --setProperty format png \
        --cropToHeightWidth "$crop_h" "$crop_w" \
        "$source_image" --out "$framed" >/dev/null
      ;;
  esac
  echo "framed: central ${zoom}% of ${src_w}x${src_h} -> ${crop_w}x${crop_h}"
fi

square() { # square SIZE OUTPUT
  local size=$1 dest=$2
  case $tool in
    magick)
      magick "$framed" -resize "${size}x${size}!" -strip "PNG24:$dest"
      ;;
    sips)
      sips --setProperty format png \
        --resampleHeightWidth "$size" "$size" \
        "$framed" --out "$dest" >/dev/null
      ;;
  esac
}

pad() { # pad SOURCE HEIGHT WIDTH OUTPUT
  local src=$1 height=$2 width=$3 dest=$4
  case $tool in
    magick)
      magick "$src" -background "#$background" -gravity center \
        -extent "${width}x${height}" -strip "PNG24:$dest"
      ;;
    sips)
      # sips prints the resolved CGColor of --padColor on stderr; it is not a
      # failure and it reads like one, so it goes nowhere.
      sips --padToHeightWidth "$height" "$width" --padColor "$background" \
        "$src" --out "$dest" >/dev/null 2>&1
      ;;
  esac
}

echo "source: $source_image"
echo "canvas: #$background"
for spec in 16:favicon-16.png 32:favicon-32.png 180:apple-touch-icon.png \
  192:icon-192.png 512:icon-512.png; do
  size=${spec%%:*}
  name=${spec#*:}
  square "$size" "$out/$name"
  echo "  wrote public/$name (${size}x${size})"
done

# A maskable icon is cropped to a circle by the launcher, so the mark has to sit
# inside the inner 80%. That means the framed image at 410px on a 512px field.
square 410 "$work/maskable.png"
pad "$work/maskable.png" 512 512 "$out/icon-maskable-512.png"
echo "  wrote public/icon-maskable-512.png (512x512, mark inside the safe area)"

# og.png is deliberately not written here. The card is no longer the bare mark
# on a canvas: it carries the wordmark, the headline and the tagline, set in the
# Geist the site ships. Setting type is what this script cannot do, so the card
# is rendered from landing/scripts/social-card.html in a browser and committed
# like every other asset here. Generating it from the mark again would silently
# replace a card that has words on it with one that has none.

if $drop_svg; then
  rm -f "$out/favicon.svg"
  echo "  removed public/favicon.svg"
  echo
  echo "favicon.svg is gone, so drop its <link rel=icon type=image/svg+xml>"
  echo "from landing/src/components/landing/Layout.astro and set the Starlight"
  echo "favicon in landing/astro.config.mjs to /favicon-32.png."
fi

echo
echo "done with $tool. Commit landing/public/."
