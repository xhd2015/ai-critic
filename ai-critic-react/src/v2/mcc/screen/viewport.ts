/**
 * Geometry for the mirror: where the streamed image sits inside the stage, and
 * how a zoom/pan view maps stream pixels to screen pixels.
 *
 * Everything here is expressed in CSS pixels of the stage and pixels of the
 * captured image, never in device pixels, so the maths stays device-pixel-ratio
 * free and can be unit tested without a DOM. `RemoteScreen` multiplies by the
 * device pixel ratio only when it draws.
 */

export const MIN_ZOOM = 1;
export const MAX_ZOOM = 6;
/**
 * The lowest zoom the browser viewer offers: 50% of fit.
 *
 * It is a separate floor rather than a new MIN_ZOOM because this module is
 * shared with the remote screen page, where "smaller than fit" has no meaning
 * and would silently become a new capability. Every function that clamps takes
 * an optional floor that defaults to MIN_ZOOM, so that page is unchanged.
 */
export const BROWSER_MIN_ZOOM = 0.5;
/** One tap on ＋ / − changes the zoom by this factor. */
export const ZOOM_STEP = 1.5;
/** Double tap toggles between MIN_ZOOM and this. */
export const DOUBLE_TAP_ZOOM = 2.5;

export interface Point {
    x: number;
    y: number;
}

/** The stage (canvas CSS box) and the image currently being streamed. */
export interface ViewGeometry {
    stageW: number;
    stageH: number;
    imgW: number;
    imgH: number;
}

/**
 * What the viewer is looking at: `zoom` is relative to the fit-to-stage scale,
 * and `cx`/`cy` is the image pixel sitting at the centre of the stage. Keeping
 * the anchor in image pixels is what makes zooming stable across rotations and
 * frame-size changes: the centre of interest stays on the same part of the Mac.
 */
export interface Viewport {
    zoom: number;
    cx: number;
    cy: number;
}

/** The untouched, letterboxed layout of the image inside the stage. */
export interface Fit {
    scale: number;
    drawW: number;
    drawH: number;
    offX: number;
    offY: number;
}

/**
 * The slice of the image to paint and where to paint it. When the view covers
 * the whole image this is exactly the fit rect, so 1x rendering is unchanged.
 */
export interface DrawRect {
    sx: number;
    sy: number;
    sw: number;
    sh: number;
    dx: number;
    dy: number;
    dw: number;
    dh: number;
}

function clamp(value: number, lo: number, hi: number) {
    if (lo > hi) return (lo + hi) / 2;
    return Math.min(hi, Math.max(lo, value));
}

export function clampZoom(zoom: number, floor: number = MIN_ZOOM): number {
    if (!Number.isFinite(zoom)) return floor;
    return clamp(zoom, floor, MAX_ZOOM);
}

export function isZoomed(view: Viewport, floor: number = MIN_ZOOM): boolean {
    return view.zoom > floor + 1e-3;
}

export function fitOf(g: ViewGeometry): Fit {
    if (!(g.stageW > 0) || !(g.stageH > 0) || !(g.imgW > 0) || !(g.imgH > 0)) {
        return { scale: 0, drawW: 0, drawH: 0, offX: 0, offY: 0 };
    }
    const scale = Math.min(g.stageW / g.imgW, g.stageH / g.imgH);
    const drawW = g.imgW * scale;
    const drawH = g.imgH * scale;
    return { scale, drawW, drawH, offX: (g.stageW - drawW) / 2, offY: (g.stageH - drawH) / 2 };
}

/** Screen (CSS) pixels per image pixel at the current view. */
export function scaleOf(g: ViewGeometry, view: Viewport, floor: number = MIN_ZOOM): number {
    return fitOf(g).scale * clampZoom(view.zoom, floor);
}

export function centreView(g: ViewGeometry, floor: number = MIN_ZOOM): Viewport {
    return { zoom: floor, cx: g.imgW / 2, cy: g.imgH / 2 };
}

/**
 * Pulls the centre back inside the image so no empty band is ever shown, and
 * re-centres an axis the image is too small to fill (the letterboxed axis).
 */
export function clampView(g: ViewGeometry, view: Viewport, floor: number = MIN_ZOOM): Viewport {
    const zoom = clampZoom(view.zoom, floor);
    const k = scaleOf(g, { ...view, zoom }, floor);
    const cx = Number.isFinite(view.cx) ? view.cx : g.imgW / 2;
    const cy = Number.isFinite(view.cy) ? view.cy : g.imgH / 2;
    if (!k) return { zoom, cx: g.imgW / 2, cy: g.imgH / 2 };
    const halfW = g.stageW / (2 * k);
    const halfH = g.stageH / (2 * k);
    return {
        zoom,
        cx: halfW * 2 >= g.imgW ? g.imgW / 2 : clamp(cx, halfW, g.imgW - halfW),
        cy: halfH * 2 >= g.imgH ? g.imgH / 2 : clamp(cy, halfH, g.imgH - halfH),
    };
}

/**
 * Zooms to `zoom` while keeping `anchor` (image pixels) pinned to the same spot
 * on screen — that is what makes a pinch feel like it is grabbing the picture
 * rather than the viewport.
 */
export function zoomAt(g: ViewGeometry, view: Viewport, zoom: number, anchor: Point, floor: number = MIN_ZOOM): Viewport {
    const next = clampZoom(zoom, floor);
    const k0 = scaleOf(g, view, floor);
    const k1 = scaleOf(g, { ...view, zoom: next }, floor);
    if (!k0 || !k1) return clampView(g, { ...view, zoom: next }, floor);
    const ratio = k0 / k1;
    const cx = anchor.x - (anchor.x - view.cx) * ratio;
    const cy = anchor.y - (anchor.y - view.cy) * ratio;
    return clampView(g, { zoom: next, cx, cy }, floor);
}

/** Drags the picture with the finger: content follows the pointer. */
export function panBy(g: ViewGeometry, view: Viewport, dxScreen: number, dyScreen: number, floor: number = MIN_ZOOM): Viewport {
    const k = scaleOf(g, view, floor);
    if (!k) return view;
    return clampView(g, { ...view, cx: view.cx - dxScreen / k, cy: view.cy - dyScreen / k }, floor);
}

export function screenToStream(g: ViewGeometry, view: Viewport, screen: Point): Point {
    const k = scaleOf(g, view);
    if (!k) return { x: 0, y: 0 };
    return {
        x: view.cx + (screen.x - g.stageW / 2) / k,
        y: view.cy + (screen.y - g.stageH / 2) / k,
    };
}

export function streamToScreen(g: ViewGeometry, view: Viewport, stream: Point): Point {
    const k = scaleOf(g, view);
    return {
        x: g.stageW / 2 + (stream.x - view.cx) * k,
        y: g.stageH / 2 + (stream.y - view.cy) * k,
    };
}

/** Where a screen point lands inside the image, clamped to its bounds. */
export function screenToImage(g: ViewGeometry, view: Viewport, screen: Point): Point | null {
    if (!(g.imgW > 0) || !(g.imgH > 0)) return null;
    const p = screenToStream(g, view, screen);
    if (!Number.isFinite(p.x) || !Number.isFinite(p.y)) return null;
    // Touches in the letterbox band belong to the stage, not to the Mac: while
    // zoomed the band is gone, but at 1x this ignores taps on the black border.
    if (p.x < 0 || p.y < 0 || p.x > g.imgW || p.y > g.imgH) return null;
    return {
        x: Math.max(0, Math.min(g.imgW - 1, Math.round(p.x))),
        y: Math.max(0, Math.min(g.imgH - 1, Math.round(p.y))),
    };
}

export function drawRectOf(g: ViewGeometry, view: Viewport): DrawRect {
    const settled = clampView(g, view);
    const k = scaleOf(g, settled);
    if (!k) return { sx: 0, sy: 0, sw: 0, sh: 0, dx: 0, dy: 0, dw: 0, dh: 0 };
    const visW = g.stageW / k;
    const visH = g.stageH / k;
    let sx = settled.cx - visW / 2;
    let sy = settled.cy - visH / 2;
    let sw = visW;
    let sh = visH;
    if (sx < 0) {
        sw += sx;
        sx = 0;
    }
    if (sy < 0) {
        sh += sy;
        sy = 0;
    }
    if (sx + sw > g.imgW) sw = g.imgW - sx;
    if (sy + sh > g.imgH) sh = g.imgH - sy;
    return {
        sx,
        sy,
        sw,
        sh,
        dx: g.stageW / 2 + (sx - settled.cx) * k,
        dy: g.stageH / 2 + (sy - settled.cy) * k,
        dw: sw * k,
        dh: sh * k,
    };
}

/** Next zoom level for a ＋ / − tap. */
export function stepZoom(zoom: number, direction: 1 | -1, floor: number = MIN_ZOOM): number {
    return clampZoom(direction > 0 ? zoom * ZOOM_STEP : zoom / ZOOM_STEP, floor);
}

/** Double tap: snap back to fit, or into a comfortable magnified view. */
export function toggleZoom(zoom: number, floor: number = MIN_ZOOM): number {
    return zoom > floor + 1e-3 ? floor : DOUBLE_TAP_ZOOM;
}
