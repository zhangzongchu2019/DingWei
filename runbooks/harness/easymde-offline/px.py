#!/usr/bin/env python3
# 🔵 像素层量尺:给一张 PNG,回 JSON。
#    为什么要有像素层:computed style 只会告诉你"0.55 的黑",**不会**告诉你压在某个背景上最终是什么颜色
#    (本席在 0.2.1.7 遮罩那格栽过这条 ⇒ 半透明/合成后的颜色**只能实测像素**)。
# 用法: python3 px.py <png>
# 口径: bg      = 出现次数最多的颜色(即"背景")
#       darkest / lightest = 全图最暗 / 最亮像素(按 WCAG 相对亮度排,不是按 RGB 求和)
#       ratio_bg_darkest / ratio_bg_lightest = 两个方向的对比度;文字取**较远的那个**(contrast_max)
import sys, json
from collections import Counter

def lin(c):
    c = c / 255.0
    return c / 12.92 if c <= 0.03928 else ((c + 0.055) / 1.055) ** 2.4

def lum(rgb):
    r, g, b = rgb
    return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b)

def ratio(a, b):
    la, lb = lum(a), lum(b)
    hi, lo = max(la, lb), min(la, lb)
    return (hi + 0.05) / (lo + 0.05)

def main():
    from PIL import Image
    im = Image.open(sys.argv[1]).convert('RGB')
    px = list(im.getdata())
    c = Counter(px)
    bg, bgn = c.most_common(1)[0]
    colors = list(c.keys())
    darkest = min(colors, key=lum)
    lightest = max(colors, key=lum)
    rd, rl = ratio(bg, darkest), ratio(bg, lightest)
    print(json.dumps({
        'w': im.size[0], 'h': im.size[1], 'px': len(px),
        'bg': list(bg), 'bgpx': bgn, 'bgfrac': round(bgn / len(px), 4),
        'distinct': len(colors),
        'darkest': list(darkest), 'lightest': list(lightest),
        'ratio_bg_darkest': round(rd, 3), 'ratio_bg_lightest': round(rl, 3),
        'contrast_max': round(max(rd, rl), 3),
        'ink_px': len(px) - bgn,
    }))

main()
