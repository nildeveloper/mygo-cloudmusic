package systemplayer

// Reserve room for the controls and other applications' menu bar items. Long
// lyrics scroll inside this limit instead of making the whole status item vanish.
const menuLyricLimit = 120.0
const menuButtonWidth = 24.0

func menuLyricWidth(textWidth float64) float64 {
	return min(menuLyricLimit, max(0, textWidth))
}
