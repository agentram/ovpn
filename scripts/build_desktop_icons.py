#!/usr/bin/env python3
"""Render native icons from the single Desk brand SVG (ImageMagick and iconutil)."""
import hashlib
from pathlib import Path
import shutil
import subprocess
import tempfile
import xml.etree.ElementTree as ET

root = Path(__file__).resolve().parents[1]
source = root/'apps/desktop/frontend/public/brand/ovpn.svg'
assets = root/'apps/desktop/assets'
mark = ET.fromstring(source.read_text()).find('{http://www.w3.org/2000/svg}path')
if mark is None:
    raise SystemExit('Brand SVG has no path.')
with tempfile.TemporaryDirectory(prefix='ovpn-icons-') as temporary:
    temporary = Path(temporary)
    icon = temporary/'icon.svg'
    icon.write_text('<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 1024 1024">'
                    '<rect width="1024" height="1024" rx="216" fill="#111719"/>'
                    '<g transform="translate(232 180) scale(5.6)"><path fill="#f5f7f8" d="'+mark.attrib['d']+'"/></g></svg>')
    subprocess.run(['magick','-background','none',str(icon),'-strip',str(assets/'icon.png')],check=True)
    subprocess.run(['magick',str(assets/'icon.png'),'-strip','-define','icon:auto-resize=256,128,64,48,32,16',str(assets/'icon.ico')],check=True)
    if shutil.which('iconutil'):
        iconset=temporary/'ovpn.iconset'
        iconset.mkdir()
        for size in [16,32,128,256,512]:
            for scale in [1,2]:
                name=f'icon_{size}x{size}'+('@2x' if scale==2 else '')+'.png'
                subprocess.run(['magick',str(assets/'icon.png'),'-resize',str(size*scale),'-strip',str(iconset/name)],check=True)
        subprocess.run(['iconutil','-c','icns',str(iconset),'-o',str(assets/'icon.icns')],check=True)
    else:
        raise SystemExit('Regenerate macOS icons on macOS with iconutil before updating the brand digest.')
(assets/'brand.sha256').write_text(hashlib.sha256(source.read_bytes()).hexdigest()+'\n')
