import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const tree = readFileSync(join(root, 'src/v2/mcc/home/SystemPidTree.tsx'), 'utf8');
const css = readFileSync(join(root, 'src/v2/mcc/home/SystemView.css'), 'utf8');
const page = readFileSync(join(root, 'src/v2/mcc/home/SystemGroupPage.tsx'), 'utf8');

test('group-detail body toggles the subtree and never navigates', () => {
    const bodyBtn = tree.match(/className="mcc-ptbody"[\s\S]*?onClick=\{\(\) => onToggle\(proc\.pid\)\}/);
    assert.ok(bodyBtn, 'mcc-ptbody must call onToggle');
    assert.doesNotMatch(bodyBtn[0], /onOpen/);
    assert.doesNotMatch(tree, /mcc-ptmain/);
});

test('› is the only row control that opens the pid page', () => {
    assert.match(tree, /className="mcc-ptopen"/);
    assert.match(tree, /onClick=\{\(\) => onOpen\(proc\)\}/);
    assert.match(tree, /aria-label=\{openLabel\}/);
    assert.equal([...tree.matchAll(/onOpen\(proc\)/g)].length, 1);
});

test('a leaf row body is a div, not a button', () => {
    assert.match(tree, /<div className="mcc-ptbody leaf">/);
    assert.match(tree, /hasChildren \? \(/);
});

test('the row grid is body | open | kill', () => {
    assert.match(css, /\.mcc-ptrow \{[\s\S]*?grid-template-columns:\s*minmax\(0,\s*1fr\)\s+28px\s+28px/);
    assert.match(css, /\.mcc-ptbody \{[\s\S]*?grid-template-columns:\s*16px\s+minmax\(0,\s*1fr\)\s+auto\s+auto/);
});

test('the group page tip tells people to fold the row and use › to open', () => {
    assert.match(page, /Tap a row to fold its subtree; use › to open the pid page/);
    assert.doesNotMatch(page, /Tap a row for the full pid page/);
});
