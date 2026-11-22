import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    BROWSER_MIN_ZOOM,
    MIN_ZOOM,
    clampView,
    clampZoom,
    centreView,
} from '../src/v2/mcc/screen/viewport.ts';
import { PHONE_ZOOMS, nextPhoneZoom, prevPhoneZoom, zoomPercent } from '../src/v2/mcc/browser/phoneZoom.ts';
import { geometryOf, resetView, settleView, transformOf, normalizeAt } from '../src/v2/mcc/browser/browserView.ts';

// Stage 390x600 holding a 780x1200 frame: fit scale is exactly 0.5, so zoom 0.5
// paints the picture at a quarter of a screen pixel per image pixel.
const g = geometryOf({ left: 0, top: 0, width: 390, height: 600 }, 780, 1200);

test('the detents reach 50% but no further', () => {
    assert.equal(PHONE_ZOOMS[0], 0.5);
    assert.equal(PHONE_ZOOMS[1], 0.75);
    // 100% is still a detent: fit must stay one press away in both directions.
    assert.ok(PHONE_ZOOMS.includes(1));
    assert.equal(prevPhoneZoom(1), 0.75);
    assert.equal(prevPhoneZoom(0.75), 0.5);
    assert.equal(prevPhoneZoom(0.5), null, '50% is the floor');
    assert.equal(nextPhoneZoom(0.5), 0.75);
});

test('a pinch between detents snaps the right way', () => {
    // A float from a pinch must still find its neighbours.
    assert.equal(prevPhoneZoom(0.9), 0.75);
    assert.equal(nextPhoneZoom(0.9), 1);
    assert.equal(prevPhoneZoom(0.6), 0.5);
    assert.equal(prevPhoneZoom(0.55), 0.5);
});

test('the percent label names a sub-100% zoom', () => {
    assert.equal(zoomPercent(0.5), '50%');
    assert.equal(zoomPercent(0.75), '75%');
    assert.equal(zoomPercent(1), '100%');
});

test('the browser floor lets zoom go below fit', () => {
    assert.equal(clampZoom(0.4, BROWSER_MIN_ZOOM), 0.5);
    assert.equal(clampZoom(0.5, BROWSER_MIN_ZOOM), 0.5);
    assert.equal(clampZoom(0.75, BROWSER_MIN_ZOOM), 0.75);
    // Above the range it still caps at MAX_ZOOM.
    assert.equal(clampZoom(99, BROWSER_MIN_ZOOM), 6);
});

test('the screen page keeps its 100% floor', () => {
    // The shared default is what RemoteScreen relies on; a 0.5 view must come
    // back as fit there, or the screen page silently gained a new capability.
    assert.equal(clampZoom(0.5), MIN_ZOOM);
    const clamped = clampView(g, { zoom: 0.5, cx: 390, cy: 600 });
    assert.equal(clamped.zoom, MIN_ZOOM);
});

test('a 50% view is centred and actually smaller than fit', () => {
    const view = settleView(g, { zoom: 0.5, cx: 390, cy: 600 });
    assert.equal(view.zoom, 0.5, 'the floor must not snap it back to 1');
    // Centred means no translate: the element scales about the stage centre.
    const t = transformOf(g, view);
    assert.equal(t.scale, 0.5);
    assert.equal(t.tx, 0);
    assert.equal(t.ty, 0);
});

test('settling a sub-fit view keeps it inside the stage', () => {
    // Off-centre at 50%: the picture is smaller than the stage on both axes, so
    // clampView re-centres instead of letting it drift.
    const view = settleView(g, { zoom: 0.5, cx: 100, cy: 100 });
    assert.equal(view.zoom, 0.5);
    assert.equal(view.cx, 390);
    assert.equal(view.cy, 600);
});

test('resetView returns to fit, not to the floor', () => {
    const view = resetView(g);
    assert.equal(view.zoom, 1);
    assert.equal(view.cx, 390);
    assert.equal(view.cy, 600);
});

test('taps map correctly while zoomed out', () => {
    // At 50% the picture is half the fitted size, so a tap at the stage centre
    // is still the picture centre.
    const centre = normalizeAt(g, { zoom: 0.5, cx: 390, cy: 600 }, { x: 195, y: 300 });
    assert.ok(Math.abs(centre.x - 0.5) < 0.01);
    assert.ok(Math.abs(centre.y - 0.5) < 0.01);
    // A tap at the stage edge is outside the smaller picture; it clamps.
    const edge = normalizeAt(g, { zoom: 0.5, cx: 390, cy: 600 }, { x: 0, y: 0 });
    assert.equal(edge.x, 0);
    assert.equal(edge.y, 0);
});

test('centreView defaults to fit and accepts a floor', () => {
    assert.equal(centreView(g).zoom, 1);
    assert.equal(centreView(g, BROWSER_MIN_ZOOM).zoom, 0.5);
});
