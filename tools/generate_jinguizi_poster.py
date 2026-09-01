#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
生成 金龟子黄金现货模拟选拔赛 海报
基于 999.jpg 万盛鸿海报的内容，改为黄金现货主题。
"""
import os
from PIL import Image, ImageDraw, ImageFont, ImageFilter

WIDTH, HEIGHT = 1080, 2580
OUT_PATH = "D:/tools/Jinguizigoldtrader/金龟子黄金现货模拟选拔赛海报.png"
LOGO_PATH = "D:/tools/Jinguizigoldtrader/金龟子商标.png"

# 颜色
BG_TOP = (12, 26, 52)
BG_BOTTOM = (35, 20, 8)
GOLD = (212, 175, 55)
GOLD_LIGHT = (255, 220, 130)
WHITE = (255, 255, 255)
LIGHT_GRAY = (220, 220, 220)
DARK_BLUE = (18, 38, 75)
PANEL_BG = (255, 255, 255, 28)
RED = (231, 76, 60)


def get_fonts():
    """尝试加载系统中文字体。"""
    candidates = [
        "C:/Windows/Fonts/msyhbd.ttc",
        "C:/Windows/Fonts/msyh.ttc",
        "C:/Windows/Fonts/simhei.ttf",
        "C:/Windows/Fonts/simsun.ttc",
    ]
    font_path = None
    for c in candidates:
        if os.path.exists(c):
            font_path = c
            break
    if not font_path:
        raise RuntimeError("未找到中文字体")
    return {
        "title": ImageFont.truetype(font_path, 84),
        "subtitle": ImageFont.truetype(font_path, 40),
        "section": ImageFont.truetype(font_path, 44),
        "body": ImageFont.truetype(font_path, 28),
        "body_bold": ImageFont.truetype(font_path, 30),
        "table": ImageFont.truetype(font_path, 22),
        "table_header": ImageFont.truetype(font_path, 24),
        "footer": ImageFont.truetype(font_path, 34),
        "small": ImageFont.truetype(font_path, 24),
    }


def make_gradient_bg(w, h):
    img = Image.new("RGB", (w, h), BG_TOP)
    draw = ImageDraw.Draw(img)
    for y in range(h):
        ratio = y / h
        r = int(BG_TOP[0] + (BG_BOTTOM[0] - BG_TOP[0]) * ratio)
        g = int(BG_TOP[1] + (BG_BOTTOM[1] - BG_TOP[1]) * ratio)
        b = int(BG_TOP[2] + (BG_BOTTOM[2] - BG_TOP[2]) * ratio)
        draw.line([(0, y), (w, y)], fill=(r, g, b))
    return img


def add_gold_glow(draw, x, y, w, h, blur=20):
    pass  # 简化：后续可用 overlay


def draw_text_wrap(draw, text, x, y, max_w, font, fill, line_spacing=10):
    """按最大宽度自动换行绘制文本，返回总高度。"""
    chars = list(text)
    lines = []
    line = ""
    for ch in chars:
        test = line + ch
        bbox = draw.textbbox((0, 0), test, font=font)
        if bbox[2] - bbox[0] > max_w and line:
            lines.append(line)
            line = ch
        else:
            line = test
    if line:
        lines.append(line)
    for i, ln in enumerate(lines):
        draw.text((x, y + i * (font.size + line_spacing)), ln, font=font, fill=fill)
    return len(lines) * (font.size + line_spacing)


def draw_panel(draw, x, y, w, h, radius=16, outline=GOLD, fill=(255, 255, 255, 35)):
    draw.rounded_rectangle([x, y, x + w, y + h], radius=radius, outline=outline, width=2, fill=fill)


def draw_section_header(draw, x, y, w, title, fonts):
    h = 66
    # 渐变条
    draw.rounded_rectangle([x, y, x + w, y + h], radius=10, fill=DARK_BLUE, outline=GOLD, width=2)
    draw.text((x + 20, y + 10), title, font=fonts["section"], fill=GOLD_LIGHT)
    return y + h + 20


def draw_table(draw, x, y, w, header, rows, fonts):
    """绘制简单表格。header: list[str]; rows: list[list[str]]."""
    col_w = w // len(header)
    row_h = 44
    # 表头
    draw.rectangle([x, y, x + w, y + row_h], fill=(GOLD[0], GOLD[1], GOLD[2], 60))
    for i, h in enumerate(header):
        draw.text((x + i * col_w + 8, y + 8), h, font=fonts["table_header"], fill=(20, 20, 20))
    y += row_h
    # 行
    for row in rows:
        for i, cell in enumerate(row):
            draw.text((x + i * col_w + 8, y + 10), cell, font=fonts["table"], fill=WHITE)
        y += row_h
    return y


def main():
    fonts = get_fonts()
    img = make_gradient_bg(WIDTH, HEIGHT)
    draw = ImageDraw.Draw(img)

    # 顶部装饰线
    draw.rectangle([0, 0, WIDTH, 8], fill=GOLD)

    # Logo（加金色圆角底托）
    logo_y = 50
    if os.path.exists(LOGO_PATH):
        logo = Image.open(LOGO_PATH).convert("RGBA")
        logo_w = 220
        logo_h = int(logo.height * logo_w / logo.width)
        logo = logo.resize((logo_w, logo_h), Image.LANCZOS)
        lx = (WIDTH - logo_w) // 2
        badge_pad = 16
        badge_w = logo_w + badge_pad * 2
        badge_h = logo_h + badge_pad * 2
        bx = (WIDTH - badge_w) // 2
        draw.rounded_rectangle([bx, logo_y, bx + badge_w, logo_y + badge_h], radius=20, fill=WHITE, outline=GOLD, width=3)
        img.paste(logo, (lx, logo_y + badge_pad), logo)
        logo_y += badge_h + 30
    else:
        logo_y += 40

    # 主标题
    title = "金龟子黄金现货模拟选拔赛"
    bbox = draw.textbbox((0, 0), title, font=fonts["title"])
    tx = (WIDTH - (bbox[2] - bbox[0])) // 2
    draw.text((tx, logo_y), title, font=fonts["title"], fill=GOLD)
    logo_y += 110

    # 副标题
    sub = "2%经典选拔  ·  真实伦敦金行情  ·  零风险练盘  ·  6个月赛期"
    bbox = draw.textbbox((0, 0), sub, font=fonts["subtitle"])
    sx = (WIDTH - (bbox[2] - bbox[0])) // 2
    draw.text((sx, logo_y), sub, font=fonts["subtitle"], fill=LIGHT_GRAY)
    logo_y += 80

    margin = 50
    content_w = WIDTH - margin * 2
    cy = logo_y

    # 赛事宗旨（金色高亮横幅）
    mission = "赛事宗旨：以真实伦敦金行情为练兵场，在零风险模拟中训练盘手看盘、择时与严格风控的本领，系统提升交易水平与稳定盈利能力，选拔并培养真正合格的交易人才。"
    panel_h = 150
    draw.rounded_rectangle([margin, cy, WIDTH - margin, cy + panel_h], radius=16, outline=GOLD, width=2, fill=(255, 215, 110, 40))
    draw.text((margin + 18, cy + 14), "赛事宗旨", font=fonts["section"], fill=GOLD_LIGHT)
    draw_text_wrap(draw, mission, margin + 18, cy + 64, content_w - 36, fonts["body"], WHITE, line_spacing=8)
    cy += panel_h + 24

    # 1. 风控规则
    cy = draw_section_header(draw, margin, cy, content_w, "1  风控规则", fonts)
    rules = [
        "1. 公司为报名盘手提供 100万 / 500万 / 1000万 金龟子模拟账户（与平台普通模拟资金隔离）。",
        "2. 单日结算盈利大于初始本金 2% 的按 2% 计算；小于 2% 或亏损的按实际计算；6个月内盈利率随时达到 29% 以上即为通过。",
        "3. 账户历史最高动态权益盘中回撤不得超过 6%。（例：盘中最高权益 110万，则 110万×6%=6.6万，盘中亏损 6.6万即总权益低于 103.4万，账户淘汰）",
        "4. 盘中初始本金的动态回撤不得超过 5%。（例：100万账户权益低于 95万，账户淘汰）",
        "5. 第一个月账面权益盈利须达 1%（未达标提前淘汰）；第三个月盈利 10%；第六个月盈利 29%（达标即通过）。",
    ]
    for r in rules:
        h = draw_text_wrap(draw, r, margin + 20, cy, content_w - 40, fonts["body"], WHITE, line_spacing=8)
        cy += h + 14
    cy += 10

    # 2. 达标奖励
    cy = draw_section_header(draw, margin, cy, content_w, "2  达标奖励", fonts)
    reward_texts = [
        "奖励公式：达标奖励 = (档位基数 + 账户盈利率) × 管理费(200元)；三档均额外 6% 退回管理费(12元)。",
        "账户盈利率 = 期末动态权益 ÷ 初始本金 − 1。",
        " ",
        "· 200元档（小账户 100万）：基数 0",
        "  达标奖励 = 账户盈利率 × 200元。",
        "  例：盈利率 20% → 奖金 0.20 × 200 = 40元。",
        " ",
        "· 1000元档（中账户 500万）：基数 1",
        "  达标奖励 = (1 + 账户盈利率) × 200元。",
        "  例：盈利率 20% → 奖金 (1+20%) × 200 = 240元。",
        " ",
        "· 2000元档（大账户 1000万）：基数 2",
        "  达标奖励 = (2 + 账户盈利率) × 200元。",
        "  例：盈利率 20% → 奖金 (2+20%) × 200 = 440元。",
    ]
    for t in reward_texts:
        h = draw_text_wrap(draw, t, margin + 20, cy, content_w - 40, fonts["body"], WHITE, line_spacing=6)
        cy += h + 8
    cy += 10

    # 3. 淘汰条件（新增）
    cy = draw_section_header(draw, margin, cy, content_w, "3  淘汰条件", fonts)
    eliminations = [
        "· 盘中回撤触发：较历史最高动态权益回撤 ≥ 6%，即淘汰。",
        "· 本金回撤触发：较初始本金回撤 ≥ 5%，即淘汰。",
        "· 阶段盈利未达标：第一个月未盈利 1%、第三个月未盈利 10%、第六个月未盈利 29%，均淘汰。",
        "· 信息造假：参赛信息不真实，或虚假报名，取消资格并淘汰。",
        "· 主动退赛：对服务不满意可在 15天内无理由退款（需账户未交易）。",
    ]
    for t in eliminations:
        h = draw_text_wrap(draw, t, margin + 20, cy, content_w - 40, fonts["body_bold"], (255, 200, 180), line_spacing=8)
        cy += h + 10
    cy += 10

    # 4. 参赛资金说明（新增）
    cy = draw_section_header(draw, margin, cy, content_w, "4  参赛资金说明", fonts)
    fund_text = (
        "本次比赛使用独立的「金龟子模拟币」，与平台现有普通模拟资金完全隔离；"
        "报名成功后由管理员统一为参赛人员充值对应档位资金，专款专用，比赛结束后按规则结算。"
    )
    h = draw_text_wrap(draw, fund_text, margin + 20, cy, content_w - 40, fonts["body"], WHITE, line_spacing=8)
    cy += h + 20

    # 5. 注意事项
    cy = draw_section_header(draw, margin, cy, content_w, "5  注意事项", fonts)
    notes = [
        "1. 报名盘手须为完全民事行为能力人，充分知晓并理解模拟考核难度与风险。",
        "2. 自愿缴纳 200元 / 1000元 / 2000元 咨询服务费（含账户管理、每日数据统计、档案整理、选拔答疑等服务）；15天内未交易可无理由退款。",
        "3. 通过考核后提供实盘账户交由通过盘手操作，实盘账户按交易所最低手续费与保证金标准执行，盘手不承担风险、无需额外缴费。",
        "4. 对接实盘账户盈利可随时提出五五分红；实盘盈利大于 10% 可自愿申请账户初始资金翻倍。",
    ]
    for n in notes:
        h = draw_text_wrap(draw, n, margin + 20, cy, content_w - 40, fonts["body"], LIGHT_GRAY, line_spacing=8)
        cy += h + 10
    cy += 30

    # 底部横幅
    footer_h = 90
    draw.rectangle([0, HEIGHT - footer_h, WIDTH, HEIGHT], fill=(GOLD[0], GOLD[1], GOLD[2]))
    footer = "首次参加活动赛  专享 7.5 折优惠！"
    bbox = draw.textbbox((0, 0), footer, font=fonts["footer"])
    fx = (WIDTH - (bbox[2] - bbox[0])) // 2
    draw.text((fx, HEIGHT - footer_h + 22), footer, font=fonts["footer"], fill=(20, 20, 20))

    # 保存
    img.save(OUT_PATH, "PNG", quality=95)
    print(f"海报已生成: {OUT_PATH}")


if __name__ == "__main__":
    main()
