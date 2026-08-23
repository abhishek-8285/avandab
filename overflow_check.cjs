const { chromium } = require('@playwright/test');
(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
  await page.goto('http://localhost:8080/', { waitUntil: 'networkidle' });
  await page.waitForTimeout(800);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  console.log('390 overflow', overflow);
  const bad = await page.evaluate(() => {
    const vw = document.documentElement.clientWidth;
    const out = [];
    document.querySelectorAll('*').forEach(el => {
      const r = el.getBoundingClientRect();
      if (r.right > vw + 1 || r.left < -1) {
        const cls = (el.className && typeof el.className === 'string') ? el.className.slice(0,90) : '';
        out.push(`${el.tagName}.${cls} L${Math.round(r.left)} R${Math.round(r.right)}`);
      }
    });
    return out.slice(0, 15);
  });
  console.log(bad.join('\n'));
  await page.locator('.hero-premium').screenshot({ path: '/tmp/heroel-mob-new.png' });
  await browser.close();
})();
