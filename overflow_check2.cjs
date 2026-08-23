const { chromium } = require('@playwright/test');
(async () => {
  const browser = await chromium.launch();
  for (const s of [{n:'mob',w:390,h:844},{n:'tab',w:768,h:1024},{n:'desk',w:1440,h:900}]) {
    const page = await browser.newPage({ viewport: { width: s.w, height: s.h } });
    await page.goto('http://localhost:8080/', { waitUntil: 'networkidle' });
    await page.waitForTimeout(900);
    try { await page.click('text=Accept All', { timeout: 800 }); } catch {}
    await page.waitForTimeout(300);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    console.log(s.n, 'overflow', overflow);
    await page.locator('.hero-premium').screenshot({ path: `/tmp/heroel-${s.n}-new.png` });
    await page.close();
  }
  await browser.close();
})();
