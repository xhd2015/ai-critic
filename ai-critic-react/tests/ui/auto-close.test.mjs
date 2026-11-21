import assert from 'node:assert/strict';
import { after, before, test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { createServer } from 'vite';
import puppeteer from 'puppeteer';

const root = fileURLToPath(new URL('../../', import.meta.url));
let server, browser, base;

before(async () => {
    server = await createServer({ root, server: { host: '127.0.0.1', port: 0, strictPort: false }, logLevel: 'error' });
    await server.listen();
    base = `http://127.0.0.1:${server.httpServer.address().port}/tests/ui/auto-close.html`;
    browser = await puppeteer.launch({ headless: true, executablePath: process.env.CHROME_PATH || undefined, args: ['--no-sandbox'] });
});
after(async () => { await browser?.close(); await server?.close(); });

const open = async (width = 1024, height = 900) => {
    const page = await browser.newPage();
    await page.setViewport({ width, height });
    await page.goto(base, { waitUntil: 'networkidle0' });
    return page;
};

const box = (page, selector) =>
    page.$eval(selector, (el) => {
        const r = el.getBoundingClientRect();
        return { width: r.width, height: r.height, left: r.left, right: r.right, top: r.top };
    });

test('the rail entry shows the interval and the next close', async () => {
    const page = await open();
    assert.match(await page.$eval('.bwp-autosub', (el) => el.textContent), /1h · closes in 2m/);
    const r = await box(page, '.bwp-automain');
    assert.ok(r.height >= 24 && r.width > 120, `entry too small: ${JSON.stringify(r)}`);
    await page.close();
});

test('the switch saves without opening the record', async () => {
    const page = await open();
    await page.click('.bwp-switch');
    await page.waitForFunction(() => document.querySelector('.bw-page')?.getAttribute('data-saves') === '1');
    assert.match(await page.$eval('.bwp-autosub', (el) => el.textContent), /Off · windows stay open/);
    assert.equal(await page.$('.bwp-acdlg'), null);
    await page.close();
});

test('the dialog edits the threshold and saves it once', async () => {
    const page = await open();
    await page.click('.bwp-automain');
    await page.waitForSelector('.bwp-acdlg');
    assert.match(await page.$eval('.bwp-acsecval', (el) => el.textContent), /1 hour/);
    const buttons = await page.$$('.bwp-acseg button');
    await buttons[1].click();
    assert.match(await page.$eval('.bwp-acsecval', (el) => el.textContent), /30 minutes/);
    await page.click('.bwp-acsave');
    await page.waitForFunction(() => document.querySelector('.bw-page')?.getAttribute('data-saves') === '1');
    assert.equal(await page.$('.bwp-acdlg'), null);
    assert.match(await page.$eval('.bwp-autosub', (el) => el.textContent), /30m/);
    await page.close();
});

test('copy is honest about the daemon lifetime of the record', async () => {
    const page = await open();
    await page.click('.bwp-automain');
    await page.waitForSelector('.bwp-acreclist');
    const rows = await page.$$eval('.bwp-acrecl', (els) => els.map((el) => el.textContent));
    assert.equal(rows.length, 3);
    assert.match(rows[0], /Release notes/);
    assert.match(rows[0], /tabs · idle 1h 2m/);
    assert.match(await page.$eval('.bwp-acnote', (el) => el.textContent), /daemon memory/);
    assert.match(await page.$eval('.bwp-account', (el) => el.textContent), /3 of 10/);
    await page.close();
});

test('the dialog fits a phone and its actions stay on screen', async () => {
    const page = await open(390, 844);
    await page.click('.bwp-automain');
    await page.waitForSelector('.bwp-acdlg');
    const dlg = await box(page, '.bwp-acdlg');
    assert.ok(dlg.top >= 0 && dlg.right <= 390, `dialog overflows: ${JSON.stringify(dlg)}`);
    const save = await box(page, '.bwp-acsave');
    assert.ok(save.right <= 390 && save.height >= 28, `save button unreachable: ${JSON.stringify(save)}`);
    await page.close();
});

test('escape closes the editor without saving', async () => {
    const page = await open();
    await page.click('.bwp-automain');
    await page.waitForSelector('.bwp-acdlg');
    await page.keyboard.press('Escape');
    await page.waitForFunction(() => !document.querySelector('.bwp-acdlg'));
    assert.equal(await page.$eval('.bw-page', (el) => el.getAttribute('data-saves')), '0');
    await page.close();
});

test('the phone sheet variant keeps the editor above the sheet footer', async () => {
    const page = await browser.newPage();
    await page.setViewport({ width: 390, height: 844 });
    await page.goto(`${base}?surface=phone`, { waitUntil: 'networkidle0' });
    assert.equal(await page.$eval('.bw-page', (el) => el.getAttribute('data-surface')), 'phone');
    await page.click('.bwp-automain');
    await page.waitForSelector('.bwp-acdlg');
    const dlg = await box(page, '.bwp-acdlg');
    assert.ok(dlg.right <= 390 && dlg.height <= 844, `dialog overflows: ${JSON.stringify(dlg)}`);
    const save = await box(page, '.bwp-acsave');
    assert.ok(save.top > dlg.top && save.right <= 390, `save button outside the sheet: ${JSON.stringify(save)}`);
    // The switch in the sheet footer is still reachable after closing.
    await page.keyboard.press('Escape');
    await page.waitForFunction(() => !document.querySelector('.bwp-acdlg'));
    const sw = await box(page, '.bwp-switch');
    assert.ok(sw.right <= 390 && sw.height >= 18, `switch clipped: ${JSON.stringify(sw)}`);
    await page.close();
});
