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

const open = async (width = 420, height = 900) => {
    const page = await browser.newPage();
    await page.setViewport({ width, height });
    await page.goto(base, { waitUntil: 'networkidle0' });
    return page;
};

const box = (page, selector) =>
    page.$eval(selector, (el) => {
        const r = el.getBoundingClientRect();
        return { width: r.width, height: r.height, left: r.left, right: r.right, top: r.top, bottom: r.bottom };
    });

test('the overflow menu holds exactly Help and one auto-close button', async () => {
    const page = await open();
    await page.click('.bwp-hbtn');
    await page.waitForSelector('.bwp-moremenu');
    const rows = await page.$$eval('.bwp-morerow', (els) =>
        els.map((el) => ({
            title: el.querySelector('.bwp-moretitle')?.textContent,
            pill: el.querySelector('.bwp-statepill')?.textContent ?? null,
        })),
    );
    assert.deepEqual(rows.map((row) => row.title), ['Help', 'Auto-close idle']);
    assert.equal(rows[0].pill, null, 'Help must not carry a pill');
    assert.equal(rows[1].pill, 'On · 1h');
    // No record entry, and no second control beside the row.
    assert.equal(await page.$('[data-record]'), null);
    assert.equal(await page.$$eval('.bwp-morerow .bwp-switch', (els) => els.length), 0);
    await page.close();
});

test('the pill follows the rule state', async () => {
    const page = await open();
    await page.click('.bwp-hbtn');
    await page.waitForSelector('.bwp-statepill');
    assert.equal(await page.$eval('.bwp-statepill', (el) => el.textContent), 'On · 1h');
    await page.click('[data-more="auto-close"]');
    await page.waitForSelector('.bwp-acdlg');
    await page.click('.bwp-acdlg .bwp-switch');
    await page.click('.bwp-acsave');
    await page.waitForFunction(() => document.querySelector('.bw-page')?.getAttribute('data-saves') === '1');
    await page.click('.bwp-hbtn');
    await page.waitForSelector('.bwp-statepill');
    assert.equal(await page.$eval('.bwp-statepill', (el) => el.textContent), 'Off');
    assert.match(await page.$eval('[data-more="auto-close"] .bwp-moresub', (el) => el.textContent), /Nothing closes/);
    await page.close();
});

test('clicking the row opens the config modal, not a toggle', async () => {
    const page = await open();
    await page.click('.bwp-hbtn');
    await page.waitForSelector('.bwp-moremenu');
    await page.click('[data-more="auto-close"]');
    await page.waitForSelector('.bwp-acdlg');
    assert.equal(await page.$('.bwp-moremenu'), null, 'menu closes behind the modal');
    assert.equal(await page.$eval('.bw-page', (el) => el.getAttribute('data-saves')), '0', 'opening must not save');
    await page.close();
});

test('Next 10 lists ten windows, oldest activity first', async () => {
    const page = await open();
    await page.click('.bwp-hbtn');
    await page.click('[data-more="auto-close"]');
    await page.waitForSelector('.bwp-nextlist');
    const rows = await page.$$eval('.bwp-nextrow', (els) =>
        els.map((el) => ({
            rank: el.querySelector('.bwp-nextrank')?.textContent,
            title: el.querySelector('.bwp-nexttitle')?.textContent,
            meta: el.querySelector('.bwp-nextmeta')?.textContent,
            due: el.querySelector('.bwp-due')?.textContent,
            tone: el.querySelector('.bwp-due')?.className,
        })),
    );
    assert.equal(rows.length, 10);
    assert.deepEqual(rows.map((row) => row.rank), ['1', '2', '3', '4', '5', '6', '7', '8', '9', '10']);
    assert.equal(rows[0].title, 'Linear · ENG-482');
    assert.equal(rows[9].title, 'Notion · browser spec');
    assert.match(rows[0].meta, /idle 2h · 3 tabs/);
    assert.match(rows[0].due, /due now/);
    assert.match(await page.$eval('.bwp-nextfoot', (el) => el.textContent), /10 of 11/);
    // The current window is exempt, so it never appears.
    assert.ok(!rows.some((row) => row.title === 'about:blank'));
    await page.close();
});

test('the rule off shows an empty Next 10 instead of a plan', async () => {
    const page = await open();
    await page.click('.bwp-hbtn');
    await page.click('[data-more="auto-close"]');
    await page.waitForSelector('.bwp-acdlg');
    await page.click('.bwp-acdlg .bwp-switch');
    await page.waitForFunction(() => {
        const panel = document.querySelector('.bwp-acdlg');
        return panel && panel.textContent?.includes('Rule is off');
    });
    assert.equal(await page.$('.bwp-nextrow'), null);
    await page.close();
});

test('Help closes the menu and reports back', async () => {
    const page = await open();
    await page.click('.bwp-hbtn');
    await page.click('[data-more="help"]');
    await page.waitForFunction(() => !!document.querySelector('[data-help-open]'));
    assert.equal(await page.$('.bwp-moremenu'), null);
    await page.close();
});

test('escape closes the menu before anything else', async () => {
    const page = await open();
    await page.click('.bwp-hbtn');
    await page.waitForSelector('.bwp-moremenu');
    await page.keyboard.press('Escape');
    await page.waitForFunction(() => !document.querySelector('.bwp-moremenu'));
    assert.equal(await page.$('.bwp-acdlg'), null);
    await page.close();
});

test('the popover stays inside a narrow viewport', async () => {
    const page = await open(360, 780);
    await page.click('.bwp-hbtn');
    await page.waitForSelector('.bwp-moremenu');
    const r = await box(page, '.bwp-moremenu');
    assert.ok(r.left >= 0 && r.right <= 360, `menu overflows: ${JSON.stringify(r)}`);
    assert.ok(r.top >= 0 && r.bottom <= 780, `menu off screen: ${JSON.stringify(r)}`);
    await page.close();
});
