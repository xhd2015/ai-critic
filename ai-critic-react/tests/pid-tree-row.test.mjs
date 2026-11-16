import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const tree = readFileSync(join(root, 'src/v2/mcc/home/SystemPidTree.tsx'), 'utf8');
const css = readFileSync(join(root, 'src/v2/mcc/home/SystemView.css'), 'utf8');
const page = readFileSync(join(root, 'src/v2/mcc/home/SystemGroupPage.tsx'), 'utf8');

test('group-detail tree reuses the home list cards, not a ptree skin', () => {
    assert.match(tree, /className="mcc-system-list"/);
    assert.match(tree, /className="mcc-system-group"/);
    assert.match(tree, /className="mcc-system-gwrap with-kill"/);
    assert.match(tree, /className="mcc-system-ghead"/);
    assert.match(tree, /className="mcc-system-gtree"/);
    assert.doesNotMatch(tree, /mcc-ptrow|mcc-ptbody|mcc-ptopen|mcc-ptree|ChevronRightIcon/);
    assert.doesNotMatch(css, /\.mcc-ptrow|\.mcc-ptbody|\.mcc-ptopen|\.mcc-ptree \{/);
});

test('a parent ghead toggles and never navigates', () => {
    const bodyBtn = tree.match(/className="mcc-system-ghead"[\s\S]*?onClick=\{\(\) => onToggle\(proc\.pid\)\}/);
    assert.ok(bodyBtn, 'mcc-system-ghead must call onToggle');
    assert.doesNotMatch(bodyBtn[0], /onOpen/);
});

test('gtree is the only row control that opens the pid page', () => {
    const openBtn = tree.match(/className="mcc-system-gtree"[\s\S]*?onClick=\{\(\) => onOpen\(proc\)\}/);
    assert.ok(openBtn, 'mcc-system-gtree must call onOpen');
    assert.equal([...tree.matchAll(/onOpen\(proc\)/g)].length, 1);
});

test('a leaf row body is a div, not a button', () => {
    assert.match(tree, /<div className="mcc-system-ghead leaf"/);
});

test('kill sits left of gtree so the open button stays last', () => {
    const fromWrap = tree.slice(tree.indexOf('className="mcc-system-gwrap with-kill"'));
    assert.ok(fromWrap.indexOf('mcc-system-kill') < fromWrap.indexOf('mcc-system-gtree'));
    assert.match(css, /\.mcc-system-gwrap\.with-kill \{[\s\S]*?grid-template-columns:\s*minmax\(0,\s*1fr\)\s+32px\s+32px/);
});

test('the group page tip points at the tree button, not a chevron', () => {
    assert.match(page, /Tap a parent to fold its subtree; use the tree button to open the pid page/);
    assert.doesNotMatch(page, /use › to open/);
});
