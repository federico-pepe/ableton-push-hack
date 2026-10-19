package push3

// LEDButtons is every Push 3 button that has an LED, so a hack that owns
// the surface can set all of them -- light what it gives a meaning, switch
// off the rest -- instead of leaving whatever Live last showed. It is
// push-manager's clearAllLEDs list plus the 8 scene buttons, which that
// list leaves out. Encoders, the jog wheel and the touch strip have no
// button LED.
var LEDButtons = []uint8{
	// Screen buttons, top then bottom
	CCScreenTop1, CCScreenTop2, CCScreenTop3, CCScreenTop4,
	CCScreenTop5, CCScreenTop6, CCScreenTop7, CCScreenTop8,
	CCScreenBot1, CCScreenBot2, CCScreenBot3, CCScreenBot4,
	CCScreenBot5, CCScreenBot6, CCScreenBot7, CCScreenBot8,
	// Scene / step-resolution column
	CCScene14, CCScene14t, CCScene18, CCScene18t,
	CCScene116, CCScene116t, CCScene132, CCScene132t,
	// Top-right cluster and views
	CCSet, CCSettings, CCHelp, CCUserMode,
	CCDeviceView, CCMixerView, CCClipView, CCSessionView,
	// Modifiers and edit
	CCShift, CCSelect, CCUndo, CCSave, CCAdd, CCSwap,
	// Track
	CCLock, CCStopClips, CCMute, CCSolo, CCSelectMain,
	// Transport
	CCTapTempo, CCMetronome, CCQuantize, CCFixedLength,
	CCAutomate, CCNew, CCCapture, CCRecord, CCPlay,
	// Modes
	CCRepeat, CCAccent, CCScale, CCLayout, CCNote, CCSession,
	// Loop / clip
	CCDoubleLoop, CCDuplicate, CCConvert, CCDelete,
	// Octave / page
	CCOctaveUp, CCOctaveDown, CCPageLeft, CCPageRight,
	// D-pad and jog clicks
	CCDPadUp, CCDPadRight, CCDPadDown, CCDPadLeft, CCDPadCenter,
	CCJogPress, CCJogClickLeft, CCJogClickRight,
}

// IsRGBButton reports a button whose LED takes a palette index (the
// screen rows and the scene column), as pads do. Every other button LED is
// white, and its value is a brightness, 0-127.
func IsRGBButton(cc uint8) bool {
	return (cc >= CCScreenTop1 && cc <= CCScreenTop8) ||
		(cc >= CCScreenBot1 && cc <= CCScreenBot8) ||
		(cc >= CCScene14 && cc <= CCScene132t)
}
