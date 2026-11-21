package desktop

// WaitingPage is what the window shows until the server is ready: only the background colour of the app layout (bg-bone), so that there
// is no flash of the other theme before the server answers. Light is the default and Dark follows prefers-color-scheme, as tokens.css
// does while the page has no data-theme of its own. The values are --bone of static/src/css/tokens.css (waiting_test.go compares them).
const WaitingPage = `<!doctype html><html><head><meta name="color-scheme" content="light dark"><style>` +
	`body{margin:0;background:#f5f5f4}` +
	`@media (prefers-color-scheme:dark){body{background:#050505}}` +
	`</style></head><body></body></html>`

// WindowBackground is the colour of the window itself (RGB) before the web view paints. The window has one colour whatever the theme,
// so it is the Light --bone, the default of WaitingPage; the web view covers it as soon as it draws.
var WindowBackground = [3]uint8{0xf5, 0xf5, 0xf4}
