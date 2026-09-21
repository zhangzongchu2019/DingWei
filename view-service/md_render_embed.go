package viewservice

import _ "embed"

// MarkdownRenderCore is the exact shared renderer byte stream consumed by the
// timeline sync gate and by the console spec page. Keeping the embed beside
// the asset avoids a generated second copy in the Go tree.
//
//go:embed md-render-core.js
var MarkdownRenderCore string
