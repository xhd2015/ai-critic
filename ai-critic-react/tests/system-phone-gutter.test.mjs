import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';

const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../src/v2/mcc/home/SystemView.css'), 'utf8');
const at = css.indexOf('@media (max-width: 720px)');
const phone = at >= 0 ? css.slice(at) : '';

test('phone system pages tighten horizontal gutters to 8px', () => {
    assert.ok(phone, 'expected a 720px media query');
    const block = phone;
    assert.match(block, /\.mcc-system-tiles/);
    assert.match(block, /\.mcc-system-list/);
    assert.match(block, /\.mcc-ptip/);
    assert.match(block, /\.mcc-ptools/);
    assert.match(block, /\.mcc-system \.mcc-section-header/);
    assert.match(block, /padding-left:\s*8px/);
    assert.match(block, /padding-right:\s*8px/);
    assert.match(block, /margin-left:\s*8px/);
    assert.match(block, /margin-right:\s*8px/);
});

test('desktop gutters stay 16px outside the phone query', () => {
    const before = css.slice(0, css.indexOf('@media (max-width: 720px)'));
    assert.match(before, /\.mcc-system-list \{[\s\S]*?padding:\s*0 16px/);
    assert.match(before, /\.mcc-system-tiles \{[\s\S]*?padding:\s*0 16px/);
});
