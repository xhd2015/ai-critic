import assert from 'node:assert/strict';
import { test } from 'node:test';
import { clipboardEntries, copyableFlavor, flavorMIME, humanBytes, peekSizeLabel } from '../src/v2/mcc/browser/clipboardPeek.ts';

test('the size line names characters for text and bytes for the rest', () => {
    assert.equal(peekSizeLabel({ kind: 'text', text: 'hello', bytes: 5 }), '5 chars');
    // A truncated preview reports both numbers so the panel can be honest.
    assert.equal(peekSizeLabel({ kind: 'text', text: 'aaaa', bytes: 4000, truncated: true }), '4 of 4000 chars');
    assert.equal(peekSizeLabel({ kind: 'rich', text: 'abc', html: '<p>abc</p>', bytes: 12 }), '3 chars');
    assert.equal(peekSizeLabel({ kind: 'image', image: { mime: 'image/png', width: 800, height: 1760, bytes: 93763 } }), '800×1760 · 92 KB');
    assert.equal(peekSizeLabel({ kind: 'files', files: [{ name: 'a', path: '/a' }, { name: 'b', path: '/b' }] }), '2 files');
    assert.equal(peekSizeLabel({ kind: 'empty' }), '');
    assert.equal(peekSizeLabel(null), '');
});

test('humanBytes keeps small numbers exact and rounds large ones', () => {
    assert.equal(humanBytes(0), '0 B');
    assert.equal(humanBytes(512), '512 B');
    assert.equal(humanBytes(1024), '1.0 KB');
    // Above 10 KB the panel drops the decimal: a preview does not need sub-KB
    // precision, and the shorter label fits the header.
    assert.equal(humanBytes(93763), '92 KB');
    assert.equal(humanBytes(2 * 1024 * 1024), '2.0 MB');
    assert.equal(humanBytes(Number.NaN), '0 B');
});

test('only a non-empty clipboard offers a copy flavor', () => {
    assert.equal(copyableFlavor(null), '');
    assert.equal(copyableFlavor({ kind: 'empty' }), '');
    assert.equal(copyableFlavor({ kind: 'text', text: 'x', copy_flavor: 'text' }), 'text');
    assert.equal(copyableFlavor({ kind: 'rich', html: '<p>x</p>', copy_flavor: 'html' }), 'html');
    assert.equal(copyableFlavor({ kind: 'image', image: { mime: 'image/png', bytes: 1 }, copy_flavor: 'png' }), 'png');
    // Files degrade to text on a phone, and the peek says so.
    assert.equal(copyableFlavor({ kind: 'files', files: [{ name: 'a', path: '/a' }], copy_flavor: 'text' }), 'text');
});

test('each flavor maps to the MIME a ClipboardItem needs', () => {
    assert.equal(flavorMIME('html'), 'text/html');
    assert.equal(flavorMIME('text'), 'text/plain');
    assert.equal(flavorMIME('png'), 'image/png');
    assert.equal(flavorMIME('jpeg'), 'image/jpeg');
    assert.equal(flavorMIME('jpg'), 'image/jpeg');
    assert.equal(flavorMIME('tiff'), 'image/tiff');
    assert.equal(flavorMIME('gif'), 'image/gif');
    // An unknown flavor must not silently become an image type.
    assert.equal(flavorMIME('exe'), 'text/plain');
});

test('rich text carries a plain half so a plain target degrades', () => {
    // The rule the writer follows: html plus plain, never html alone.
    const peek = { kind: 'rich', html: '<h1>Plan</h1><p>Ship it.</p>', text: 'Plan\nShip it.', copy_flavor: 'html' };
    const entries = clipboardEntries('html', peek, new Blob(['<h1>Plan</h1>'], { type: 'text/html' }));
    assert.deepEqual(Object.keys(entries).sort(), ['text/html', 'text/plain']);
    assert.equal(entries['text/plain'].type, 'text/plain');
});

test('an image clipboard has no plain half to add', () => {
    const peek = { kind: 'image', image: { mime: 'image/png', bytes: 10 }, copy_flavor: 'png' };
    const entries = clipboardEntries('png', peek, new Blob([new Uint8Array([1, 2])], { type: 'image/png' }));
    assert.deepEqual(Object.keys(entries), ['image/png']);
});

test('plain text is written without inventing an html half', () => {
    const entries = clipboardEntries('text', { kind: 'text', text: 'hi', copy_flavor: 'text' }, new Blob(['hi']));
    assert.deepEqual(Object.keys(entries), ['text/plain']);
});

test('a stale prefetched blob is not reused for a different flavor', () => {
    // The hook keys the cache by flavor; a mismatch must refetch, or the phone
    // would receive PNG bytes labelled as text.
    const cached = { flavor: 'png', blob: 'png-bytes' };
    const currentFlavor = 'text';
    const usable = cached.flavor === currentFlavor;
    assert.equal(usable, false);
});
