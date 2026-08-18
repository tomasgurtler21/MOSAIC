package mosaic

// InjectionBundle is the injection class for deployed regions filled verbatim
// from the canonical deployed-sections bundle. Bundle regions carry content that
// is common across multiple agents but varies by role; the exact block is selected
// at deploy time by BundleContent.BlockFor(target, role).
const InjectionBundle InjectionClass = "bundle"
