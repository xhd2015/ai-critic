import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const css = readFileSync(join(root, 'src/v2/MobileCodingConnector.css'), 'utf8');
const icon = readFileSync(join(root, 'src/pure-view/icons/BackIcon.tsx'), 'utf8');

test('a back button that holds an SVG is a 28×28 flex box', () => {
    assert.match(css, /\.mcc-back-btn \{[\s\S]*?display:\s*inline-flex/);
    assert.match(css, /\.mcc-back-btn:has\(svg\) \{[\s\S]*?width:\s*28px/);
    assert.match(css, /\.mcc-back-btn:has\(svg\) \{[\s\S]*?height:\s*28px/);
    assert.match(css, /\.mcc-back-btn:has\(svg\) \{[\s\S]*?padding:\s*0/);
    assert.match(css, /\.mcc-back-btn svg \{[\s\S]*?display:\s*block/);
});

test('the section title shares the 28px box height', () => {
    assert.match(css, /\.mcc-section-header h2 \{[\s\S]*?line-height:\s*28px/);
});

test('BackIcon stays 20×20 and is hidden from AT', () => {
    assert.match(icon, /width="20"/);
    assert.match(icon, /height="20"/);
    assert.match(icon, /aria-hidden="true"/);
});

test('System pages no longer override back-button padding inline', () => {
    const files = [
        'src/v2/mcc/home/SystemView.tsx',
        'src/v2/mcc/home/SystemGroupPage.tsx',
        'src/v2/mcc/home/SystemProcessPage.tsx',
        'src/v2/mcc/home/DiskView.tsx',
        'src/v2/mcc/home/ManageServerView.tsx',
        'src/v2/mcc/home/SpaceAnalysisView.tsx',
        'src/v2/mcc/home/SpaceHistoryView.tsx',
        'src/v2/mcc/home/SpaceTrendView.tsx',
    ];
    for (const rel of files) {
        const src = readFileSync(join(root, rel), 'utf8');
        assert.doesNotMatch(src, /mcc-back-btn[\s\S]{0,200}padding:\s*4/, rel);
    }
});
