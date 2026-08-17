package main

// push3_buttons.go — re-exports of core/push3's Push 3 button/encoder map.
//
// The map itself now lives in core/push3 (single source of truth shared with
// future hacks); push-manager keeps these names in package main as thin
// aliases so its ~180 existing call sites across midi.go/ui_shadow.go/
// remap.go don't need touching. See docs/push3-button-map.md for the full
// annotated map.

import "github.com/federico-pepe/ableton-push-hack/core/push3"

const (
	CCScreenTop1 = push3.CCScreenTop1
	CCScreenTop2 = push3.CCScreenTop2
	CCScreenTop3 = push3.CCScreenTop3
	CCScreenTop4 = push3.CCScreenTop4
	CCScreenTop5 = push3.CCScreenTop5
	CCScreenTop6 = push3.CCScreenTop6
	CCScreenTop7 = push3.CCScreenTop7
	CCScreenTop8 = push3.CCScreenTop8

	CCScreenBot1 = push3.CCScreenBot1
	CCScreenBot2 = push3.CCScreenBot2
	CCScreenBot3 = push3.CCScreenBot3
	CCScreenBot4 = push3.CCScreenBot4
	CCScreenBot5 = push3.CCScreenBot5
	CCScreenBot6 = push3.CCScreenBot6
	CCScreenBot7 = push3.CCScreenBot7
	CCScreenBot8 = push3.CCScreenBot8

	CCEncoder1 = push3.CCEncoder1
	CCEncoder2 = push3.CCEncoder2
	CCEncoder3 = push3.CCEncoder3
	CCEncoder4 = push3.CCEncoder4
	CCEncoder5 = push3.CCEncoder5
	CCEncoder6 = push3.CCEncoder6
	CCEncoder7 = push3.CCEncoder7
	CCEncoder8 = push3.CCEncoder8
	CCVolume   = push3.CCVolume
	CCTempo    = push3.CCTempo

	NoteEncoder1Touch = push3.NoteEncoder1Touch
	NoteEncoder2Touch = push3.NoteEncoder2Touch
	NoteEncoder3Touch = push3.NoteEncoder3Touch
	NoteEncoder4Touch = push3.NoteEncoder4Touch
	NoteEncoder5Touch = push3.NoteEncoder5Touch
	NoteEncoder6Touch = push3.NoteEncoder6Touch
	NoteEncoder7Touch = push3.NoteEncoder7Touch
	NoteEncoder8Touch = push3.NoteEncoder8Touch
	NoteVolumeTouch   = push3.NoteVolumeTouch
	NoteTempoTouch    = push3.NoteTempoTouch

	CCJogWheel      = push3.CCJogWheel
	NoteJogTouch    = push3.NoteJogTouch
	CCJogPress      = push3.CCJogPress
	CCJogClickLeft  = push3.CCJogClickLeft
	CCJogClickRight = push3.CCJogClickRight

	CCDPadUp            = push3.CCDPadUp
	CCDPadRight         = push3.CCDPadRight
	CCDPadDown          = push3.CCDPadDown
	CCDPadLeft          = push3.CCDPadLeft
	CCDPadCenter        = push3.CCDPadCenter
	NoteDPadCenterTouch = push3.NoteDPadCenterTouch

	CCSet      = push3.CCSet
	CCSettings = push3.CCSettings
	CCHelp     = push3.CCHelp
	CCUserMode = push3.CCUserMode

	CCDeviceView  = push3.CCDeviceView
	CCMixerView   = push3.CCMixerView
	CCClipView    = push3.CCClipView
	CCSessionView = push3.CCSessionView

	CCShift  = push3.CCShift
	CCSelect = push3.CCSelect

	CCUndo = push3.CCUndo
	CCSave = push3.CCSave
	CCAdd  = push3.CCAdd
	CCSwap = push3.CCSwap

	CCLock       = push3.CCLock
	CCStopClips  = push3.CCStopClips
	CCMute       = push3.CCMute
	CCSolo       = push3.CCSolo
	CCSelectMain = push3.CCSelectMain

	CCTapTempo    = push3.CCTapTempo
	CCMetronome   = push3.CCMetronome
	CCQuantize    = push3.CCQuantize
	CCFixedLength = push3.CCFixedLength
	CCAutomate    = push3.CCAutomate
	CCNew         = push3.CCNew
	CCCapture     = push3.CCCapture
	CCRecord      = push3.CCRecord
	CCPlay        = push3.CCPlay

	CCScene14   = push3.CCScene14
	CCScene14t  = push3.CCScene14t
	CCScene18   = push3.CCScene18
	CCScene18t  = push3.CCScene18t
	CCScene116  = push3.CCScene116
	CCScene116t = push3.CCScene116t
	CCScene132  = push3.CCScene132
	CCScene132t = push3.CCScene132t

	CCRepeat  = push3.CCRepeat
	CCAccent  = push3.CCAccent
	CCScale   = push3.CCScale
	CCLayout  = push3.CCLayout
	CCNote    = push3.CCNote
	CCSession = push3.CCSession

	CCDoubleLoop = push3.CCDoubleLoop
	CCDuplicate  = push3.CCDuplicate
	CCConvert    = push3.CCConvert
	CCDelete     = push3.CCDelete

	CCOctaveUp   = push3.CCOctaveUp
	CCOctaveDown = push3.CCOctaveDown
	CCPageLeft   = push3.CCPageLeft
	CCPageRight  = push3.CCPageRight

	PadNoteMin = push3.PadNoteMin
	PadNoteMax = push3.PadNoteMax
)

func PadNote(col, row int) byte          { return push3.PadNote(col, row) }
func IsPadNote(note byte) bool           { return push3.IsPadNote(note) }
func PadCoord(note byte) (col, row int)  { return push3.PadCoord(note) }
func CCScreenTopN(n int) byte            { return push3.CCScreenTopN(n) }
func CCScreenBotN(n int) byte            { return push3.CCScreenBotN(n) }
func CCEncoderN(n int) byte              { return push3.CCEncoderN(n) }
func NoteEncoderTouchN(n int) byte       { return push3.NoteEncoderTouchN(n) }
