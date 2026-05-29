#!/usr/bin/env python3
"""Render scripts/og-image.html to public/og-image.png at 1200x630.

Run with the project's Playwright environment, e.g.:
    /tmp/pwvenv/bin/python scripts/render-og.py
"""
import pathlib
from playwright.sync_api import sync_playwright

HERE = pathlib.Path(__file__).resolve().parent
SRC = HERE / "og-image.html"
OUT = HERE.parent / "public" / "og-image.png"

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page(viewport={"width": 1200, "height": 630}, device_scale_factor=1)
    page.goto(SRC.as_uri())
    page.wait_for_timeout(400)  # let webfonts + cloud images settle
    page.locator(".card").screenshot(path=str(OUT))
    browser.close()

print("wrote", OUT)
