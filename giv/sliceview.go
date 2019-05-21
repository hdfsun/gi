// Copyright (c) 2018, The GoKi Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package giv

import (
	"encoding/json"
	"fmt"
	"image"
	"log"
	"reflect"
	"sort"

	"github.com/chewxy/math32"
	"github.com/goki/gi/gi"
	"github.com/goki/gi/oswin"
	"github.com/goki/gi/oswin/dnd"
	"github.com/goki/gi/oswin/key"
	"github.com/goki/gi/oswin/mimedata"
	"github.com/goki/gi/oswin/mouse"
	"github.com/goki/gi/units"
	"github.com/goki/ki/ints"
	"github.com/goki/ki/ki"
	"github.com/goki/ki/kit"
	"github.com/goki/pi/filecat"
)

////////////////////////////////////////////////////////////////////////////////////////
//  SliceView

// SliceView represents a slice, creating a property editor of the values --
// constructs Children widgets to show the index / value pairs, within an
// overall frame. Set to Inactive for select-only mode, which emits WidgetSig
// WidgetSelected signals when selection is updated
type SliceView struct {
	gi.Frame
	Slice            interface{}        `view:"-" desc:"the slice that we are a view onto -- must be a pointer to that slice"`
	SliceValView     ValueView          `desc:"ValueView for the slice itself, if this was created within value view framework -- otherwise nil"`
	isArray          bool               `desc:"whether the slice is actually an array -- no modifications -- set by SetSlice"`
	AddOnly          bool               `desc:"can the user delete elements of the slice"`
	DeleteOnly       bool               `desc:"can the user add elements to the slice"`
	StyleFunc        SliceViewStyleFunc `view:"-" json:"-" xml:"-" desc:"optional styling function"`
	ShowViewCtxtMenu bool               `desc:"if the type we're viewing has its own CtxtMenu property defined, should we also still show the view's standard context menu?"`
	Changed          bool               `desc:"has the slice been edited?"`
	Values           []ValueView        `json:"-" xml:"-" desc:"ValueView representations of the slice values"`
	ShowIndex        bool               `xml:"index" desc:"whether to show index or not -- updated from 'index' property (bool)"`
	InactKeyNav      bool               `xml:"inact-key-nav" desc:"support key navigation when inactive (default true) -- updated from 'intact-key-nav' property (bool) -- no focus really plausible in inactive case, so it uses a low-pri capture of up / down events"`
	SelVal           interface{}        `view:"-" json:"-" xml:"-" desc:"current selection value -- initially select this value if set"`
	SelectedIdx      int                `json:"-" xml:"-" desc:"index of currently-selected item, in Inactive mode only"`
	SelectMode       bool               `desc:"editing-mode select rows mode"`
	SelectedIdxs     map[int]struct{}   `desc:"list of currently-selected slice indexes"`
	DraggedIdxs      []int              `desc:"list of currently-dragged indexes"`
	SliceViewSig     ki.Signal          `json:"-" xml:"-" desc:"slice view interactive editing signals"`
	ViewSig          ki.Signal          `json:"-" xml:"-" desc:"signal for valueview -- only one signal sent when a value has been set -- all related value views interconnect with each other to update when others update"`
	TmpSave          ValueView          `json:"-" xml:"-" desc:"value view that needs to have SaveTmp called on it whenever a change is made to one of the underlying values -- pass this down to any sub-views created from a parent"`
	ToolbarSlice     interface{}        `view:"-" desc:"the slice that we successfully set a toolbar for"`

	SliceSize    int     `view:"inactive" desc:"number of rows"`
	DispRows     int     `view:"inactive" desc:"actual number of rows displayed = min(VisRows, SliceSize)"`
	StartIdx     int     `view:"inactive" desc:"starting slice index of visible rows"`
	RowHeight    float32 `view:"inactive" desc:"height of a single row"`
	VisRows      int     `view:"inactive" desc:"total number of rows visible in allocated display size"`
	layoutHeight float32 `copy:"-" view:"-" json:"-" xml:"-" desc:"the height of grid from last layout -- determines when update needed"`
	renderedRows int     `copy:"-" view:"-" json:"-" xml:"-" desc:"the number of rows rendered -- determines update"`
	inFocusGrab  bool
	curIdx       int // temp idx variable used e.g., in Drop method
}

var KiT_SliceView = kit.Types.AddType(&SliceView{}, SliceViewProps)

// AddNewSliceView adds a new sliceview to given parent node, with given name.
func AddNewSliceView(parent ki.Ki, name string) *SliceView {
	return parent.AddNewChild(KiT_SliceView, name).(*SliceView)
}

func (sv *SliceView) Disconnect() {
	sv.Frame.Disconnect()
	sv.SliceViewSig.DisconnectAll()
	sv.ViewSig.DisconnectAll()
}

// SliceViewStyleFunc is a styling function for custom styling /
// configuration of elements in the view
type SliceViewStyleFunc func(sv *SliceView, slice interface{}, widg gi.Node2D, row int, vv ValueView)

// SetSlice sets the source slice that we are viewing -- rebuilds the children
// to represent this slice
func (sv *SliceView) SetSlice(sl interface{}, tmpSave ValueView) {
	updt := false
	if sv.Slice != sl {
		updt = sv.UpdateStart()
		sv.StartIdx = 0
		sv.Slice = sl
		sv.isArray = kit.NonPtrType(reflect.TypeOf(sl)).Kind() == reflect.Array
		if !sv.IsInactive() {
			sv.SelectedIdx = -1
		}
		sv.SelectedIdxs = make(map[int]struct{})
		sv.SelectMode = false
		sv.SetFullReRender()
	}
	sv.ShowIndex = true
	if sidxp, err := sv.PropTry("index"); err == nil {
		sv.ShowIndex, _ = kit.ToBool(sidxp)
	}
	sv.InactKeyNav = true
	if siknp, err := sv.PropTry("inact-key-nav"); err == nil {
		sv.InactKeyNav, _ = kit.ToBool(siknp)
	}
	sv.TmpSave = tmpSave
	sv.Config()
	sv.UpdateEnd(updt)
}

var SliceViewProps = ki.Props{
	"background-color": &gi.Prefs.Colors.Background,
	"max-width":        -1,
	"max-height":       -1,
}

// SliceViewSignals are signals that sliceview can send, mostly for editing
// mode.  Selection events are sent on WidgetSig WidgetSelected signals in
// both modes.
type SliceViewSignals int64

const (
	// SliceViewDoubleClicked emitted during inactive mode when item
	// double-clicked -- can be used for accepting dialog.
	SliceViewDoubleClicked SliceViewSignals = iota

	// todo: add more signals as needed

	SliceViewSignalsN
)

//go:generate stringer -type=SliceViewSignals

// UpdateValues updates the widget display of slice values, assuming same slice config
func (sv *SliceView) UpdateValues() {
	updt := sv.UpdateStart()
	for _, vv := range sv.Values {
		vv.UpdateWidget()
	}
	sv.UpdateEnd(updt)
}

// Config configures a standard setup of the overall Frame
func (sv *SliceView) Config() {
	sv.Lay = gi.LayoutVert
	sv.SetProp("spacing", gi.StdDialogVSpaceUnits)
	config := kit.TypeAndNameList{}
	config.Add(gi.KiT_ToolBar, "toolbar")
	config.Add(gi.KiT_Layout, "grid-lay")
	mods, updt := sv.ConfigChildren(config, true)

	gl := sv.GridLayout()
	gl.Lay = gi.LayoutHoriz
	gl.SetStretchMaxHeight() // for this to work, ALL layers above need it too
	gl.SetStretchMaxWidth()  // for this to work, ALL layers above need it too
	gconfig := kit.TypeAndNameList{}
	gconfig.Add(gi.KiT_Frame, "grid")
	gconfig.Add(gi.KiT_ScrollBar, "scrollbar")
	gl.ConfigChildren(gconfig, true) // covered by above

	sv.ConfigGrid()
	sv.ConfigToolbar()
	if mods {
		sv.SetFullReRender()
		sv.UpdateEnd(updt)
	}
}

// IsConfiged returns true if the widget is fully configured
func (sv *SliceView) IsConfiged() bool {
	if len(sv.Kids) == 0 {
		return false
	}
	return true
}

// GridLayout returns the Layout containing the Grid and the scrollbar
func (sv *SliceView) GridLayout() *gi.Layout {
	return sv.ChildByName("grid-lay", 0).(*gi.Layout)
}

// SliceGrid returns the SliceGrid grid frame widget, which contains all the
// fields and values
func (sv *SliceView) SliceGrid() *gi.Frame {
	return sv.GridLayout().ChildByName("grid", 0).(*gi.Frame)
}

// ScrollBar returns the SliceGrid scrollbar
func (sv *SliceView) ScrollBar() *gi.ScrollBar {
	return sv.GridLayout().ChildByName("scrollbar", 0).(*gi.ScrollBar)
}

// ToolBar returns the toolbar widget
func (sv *SliceView) ToolBar() *gi.ToolBar {
	return sv.ChildByName("toolbar", 1).(*gi.ToolBar)
}

// RowWidgetNs returns number of widgets per row and offset for index label
func (sv *SliceView) RowWidgetNs() (nWidgPerRow, idxOff int) {
	nWidgPerRow = 2
	if !sv.IsInactive() && !sv.isArray {
		if !sv.AddOnly {
			nWidgPerRow += 1
		}
		if !sv.DeleteOnly {
			nWidgPerRow += 1
		}
	}
	idxOff = 1
	if !sv.ShowIndex {
		nWidgPerRow -= 1
		idxOff = 0
	}
	return
}

// SliceValueSize returns the reflect.Value and size of the slice
// sets SliceSize always to current size
func (sv *SliceView) SliceValueSize() (reflect.Value, int) {
	svnp := kit.NonPtrValue(reflect.ValueOf(sv.Slice))
	sz := svnp.Len()
	sv.SliceSize = sz
	return svnp, sz
}

// ConfigGrid configures the SliceGrid for the current slice
func (sv *SliceView) ConfigGrid() {
	sg := sv.SliceGrid()
	updt := sg.UpdateStart()
	defer sg.UpdateEnd(updt)

	nWidgPerRow, idxOff := sv.RowWidgetNs()

	sg.Lay = gi.LayoutGrid
	sg.Stripes = gi.RowStripes
	sg.SetProp("columns", nWidgPerRow)
	// setting a pref here is key for giving it a scrollbar in larger context
	sg.SetMinPrefHeight(units.NewEm(1.5))
	sg.SetMinPrefWidth(units.NewEm(10))
	sg.SetStretchMaxHeight() // for this to work, ALL layers above need it too
	sg.SetStretchMaxWidth()  // for this to work, ALL layers above need it too

	if kit.IfaceIsNil(sv.Slice) {
		return
	}
	svnp, sz := sv.SliceValueSize()
	if sz == 0 {
		return
	}

	sg.Kids = make(ki.Slice, nWidgPerRow)

	// at this point, we make one dummy row to get size of widgets
	val := kit.OnePtrUnderlyingValue(svnp.Index(0)) // deal with pointer lists
	vv := ToValueView(val.Interface(), "")
	if vv == nil { // shouldn't happen
		return
	}
	vv.SetSliceValue(val, sv.Slice, 0, sv.TmpSave)
	vtyp := vv.WidgetType()
	itxt := fmt.Sprintf("%05d", 0)
	labnm := fmt.Sprintf("index-%v", itxt)
	valnm := fmt.Sprintf("value-%v", itxt)

	if sv.ShowIndex {
		idxlab := &gi.Label{}
		sg.SetChild(idxlab, 0, labnm)
		idxlab.Text = itxt
	}

	widg := ki.NewOfType(vtyp).(gi.Node2D)
	sg.SetChild(widg, idxOff, valnm)
	vv.ConfigWidget(widg)

	if !sv.IsInactive() && !sv.isArray {
		cidx := idxOff
		if !sv.DeleteOnly {
			addnm := fmt.Sprintf("add-%v", itxt)
			addact := gi.Action{}
			cidx += 1
			sg.SetChild(&addact, cidx, addnm)
			addact.SetIcon("plus")
		}
		if !sv.AddOnly {
			delnm := fmt.Sprintf("del-%v", itxt)
			delact := gi.Action{}
			cidx += 1
			sg.SetChild(&delact, cidx, delnm)

			delact.SetIcon("minus")
		}
	}
	sv.ConfigScroll()
}

// ConfigScroll configures the scrollbar
func (sv *SliceView) ConfigScroll() {
	sb := sv.ScrollBar()
	sb.Dim = gi.Y
	sb.Defaults()
	sb.Tracking = true
	if sv.Sty.Layout.ScrollBarWidth.Dots == 0 {
		sb.SetFixedWidth(units.NewPx(16))
	} else {
		sb.SetFixedWidth(sv.Sty.Layout.ScrollBarWidth)
	}
	sb.SetStretchMaxHeight()
	sb.Min = 0
	sb.Step = 1
	sv.UpdateScroll()

	sb.SliderSig.Connect(sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
		if sig != int64(gi.SliderValueChanged) {
			return
		}
		wupdt := sv.Viewport.Win.UpdateStart()
		sv.UpdateSliceGrid()
		// sv.SetFullReRender()
		sv.Viewport.ReRender2DNode(sv)
		sv.Viewport.Win.UpdateEnd(wupdt)
	})
}

// UpdateScroll updates grid scrollbar based on display
func (sv *SliceView) UpdateScroll() {
	sb := sv.ScrollBar()
	sb.SetFullReRender()
	updt := sb.UpdateStart()
	sb.Max = float32(sv.SliceSize)
	if sv.DispRows > 0 {
		sb.PageStep = float32(sv.DispRows) * sb.Step
		sb.ThumbVal = float32(sv.DispRows)
	} else {
		sb.PageStep = 10 * sb.Step
		sb.ThumbVal = 10
	}
	sb.TrackThr = sb.Step
	// 	sb.SetValue(float32(sv.StartIdx))
	sb.Value = float32(sv.StartIdx)
	if sv.DispRows == sv.SliceSize {
		sb.Off = true
	} else {
		sb.Off = false
	}
	sb.UpdateEnd(updt)
}

func (sv *SliceView) AvailHeight() float32 {
	sg := sv.SliceGrid()
	sgHt := sg.LayData.AllocSize.Y
	if sgHt == 0 {
		return 0
	}
	sgHt -= sg.ExtraSize.Y + sg.Sty.BoxSpace()*2
	return sgHt
}

// LayoutSliceGrid does the proper layout of slice grid depending on allocated size
// returns true if UpdateSliceGrid should be called after this
func (sv *SliceView) LayoutSliceGrid() bool {
	sg := sv.SliceGrid()
	if kit.IfaceIsNil(sv.Slice) {
		sg.DeleteChildren(true)
		return false
	}
	_, sz := sv.SliceValueSize()
	if sz == 0 {
		sg.DeleteChildren(true)
		return false
	}

	sgHt := sv.AvailHeight()
	sv.layoutHeight = sgHt
	if sgHt == 0 {
		return false
	}

	nWidgPerRow, _ := sv.RowWidgetNs()
	sv.RowHeight = sg.GridData[gi.Row][0].AllocSize + sg.Spacing.Dots
	sv.VisRows = int(math32.Floor(sgHt / sv.RowHeight))
	sv.DispRows = ints.MinInt(sv.SliceSize, sv.VisRows)

	nWidg := nWidgPerRow * sv.DispRows

	updt := sg.UpdateStart()
	defer sg.UpdateEnd(updt)
	if sv.Values == nil || sg.NumChildren() != nWidg {
		sg.DeleteChildren(true)

		sv.Values = make([]ValueView, sv.DispRows)
		sg.Kids = make(ki.Slice, nWidg)
	}
	sv.ConfigScroll()
	return true
}

func (sv *SliceView) SliceGridNeedsLayout() bool {
	sgHt := sv.AvailHeight()
	if sgHt != sv.layoutHeight {
		return true
	}
	return sv.renderedRows != sv.DispRows
}

// UpdateSliceGrid updates grid display -- robust to any time calling
func (sv *SliceView) UpdateSliceGrid() {
	if kit.IfaceIsNil(sv.Slice) {
		return
	}
	svnp, sz := sv.SliceValueSize()
	if sz == 0 {
		return
	}
	sg := sv.SliceGrid()
	sv.DispRows = ints.MinInt(sv.SliceSize, sv.VisRows)

	nWidgPerRow, idxOff := sv.RowWidgetNs()
	updt := sg.UpdateStart()
	defer sg.UpdateEnd(updt)

	nWidg := nWidgPerRow * sv.DispRows

	if sv.Values == nil || sg.NumChildren() != nWidg { // shouldn't happen..
		sv.LayoutSliceGrid()
		nWidg = nWidgPerRow * sv.DispRows
	}

	if sz > sv.DispRows {
		sb := sv.ScrollBar()
		sv.StartIdx = int(sb.Value)
		// fmt.Printf("scroll to: %v\n", sv.StartIdx)
		lastSt := sz - sv.DispRows
		sv.StartIdx = ints.MinInt(lastSt, sv.StartIdx)
		sv.StartIdx = ints.MaxInt(0, sv.StartIdx)
	} else {
		sv.StartIdx = 0
	}

	for i := 0; i < sv.DispRows; i++ {
		ridx := i * nWidgPerRow
		si := sv.StartIdx + i // slice idx
		issel := sv.IdxIsSelected(si)
		// if issel {
		// 	fmt.Printf("row: %v idx: %v is sel\n", i, si)
		// }
		val := kit.OnePtrUnderlyingValue(svnp.Index(si)) // deal with pointer lists
		vv := ToValueView(val.Interface(), "")
		if vv == nil { // shouldn't happen
			continue
		}
		vv.SetSliceValue(val, sv.Slice, si, sv.TmpSave)
		sv.Values[i] = vv
		vtyp := vv.WidgetType()
		itxt := fmt.Sprintf("%05d", i)
		sitxt := fmt.Sprintf("%05d", si)
		labnm := fmt.Sprintf("index-%v", itxt)
		valnm := fmt.Sprintf("value-%v", itxt)

		if sv.ShowIndex {
			var idxlab *gi.Label
			if sg.Kids[ridx] != nil {
				idxlab = sg.Kids[ridx].(*gi.Label)
			} else {
				idxlab = &gi.Label{}
				sg.SetChild(idxlab, ridx, labnm)
				idxlab.SetProp("slv-row", i) // all sigs deal with disp rows
				idxlab.Selectable = true
				idxlab.Redrawable = true
				idxlab.Sty.Template = "SliceView.IndexLabel"
				idxlab.WidgetSig.ConnectOnly(sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
					if sig == int64(gi.WidgetSelected) {
						wbb := send.(gi.Node2D).AsWidget()
						row := wbb.Prop("slv-row").(int)
						svv := recv.Embed(KiT_SliceView).(*SliceView)
						svv.UpdateSelectRow(row, wbb.IsSelected())
					}
				})
			}
			idxlab.CurBgColor = gi.Prefs.Colors.Background
			idxlab.SetText(sitxt)
			idxlab.SetSelectedState(issel)
		}

		var widg gi.Node2D
		if sg.Kids[ridx+idxOff] != nil {
			widg = sg.Kids[ridx+idxOff].(gi.Node2D)
			vv.ConfigWidget(widg) // note: update alone does not work here.
			if sv.IsInactive() {
				widg.AsNode2D().SetInactive()
			}
			widg.AsNode2D().SetSelectedState(issel)
		} else {
			widg = ki.NewOfType(vtyp).(gi.Node2D)
			sg.SetChild(widg, ridx+idxOff, valnm)
			vv.ConfigWidget(widg)
			wb := widg.AsWidget()
			if wb != nil {
				wb.Sty.Template = "SliceView.ItemWidget." + vtyp.Name()
			}

			if sv.IsInactive() {
				widg.AsNode2D().SetInactive()
				if wb != nil {
					wb.SetProp("slv-row", i)
					wb.ClearSelected()
					wb.WidgetSig.ConnectOnly(sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
						if sig == int64(gi.WidgetSelected) || sig == int64(gi.WidgetFocused) {
							wbb := send.(gi.Node2D).AsWidget()
							row := wbb.Prop("slv-row").(int)
							svv := recv.Embed(KiT_SliceView).(*SliceView)
							svv.UpdateSelectRow(row, wbb.IsSelected())
						}
					})
				}
			} else {
				vvb := vv.AsValueViewBase()
				vvb.ViewSig.ConnectOnly(sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
					svv, _ := recv.Embed(KiT_SliceView).(*SliceView)
					svv.SetChanged()
				})
				if !sv.isArray {
					cidx := ridx + idxOff
					if !sv.DeleteOnly {
						addnm := fmt.Sprintf("add-%v", itxt)
						addact := gi.Action{}
						cidx += 1
						sg.SetChild(&addact, cidx, addnm)

						addact.SetIcon("plus")
						addact.Tooltip = "insert a new element at this index"
						addact.Data = i
						addact.Sty.Template = "SliceView.AddAction"
						addact.ActionSig.ConnectOnly(sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
							act := send.(*gi.Action)
							svv := recv.Embed(KiT_SliceView).(*SliceView)
							svv.SliceNewAtRow(act.Data.(int)+1, true)
						})
					}

					if !sv.AddOnly {
						delnm := fmt.Sprintf("del-%v", itxt)
						delact := gi.Action{}
						cidx += 1
						sg.SetChild(&delact, cidx, delnm)

						delact.SetIcon("minus")
						delact.Tooltip = "delete this element"
						delact.Data = i
						delact.Sty.Template = "SliceView.DelAction"
						delact.ActionSig.ConnectOnly(sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
							act := send.(*gi.Action)
							svv := recv.Embed(KiT_SliceView).(*SliceView)
							svv.SliceDeleteAtRow(act.Data.(int), true)
						})
					}
				}
			}
		}
		if sv.StyleFunc != nil {
			sv.StyleFunc(sv, svnp.Interface(), widg, si, vv)
		}
	}
	if sv.SelVal != nil {
		sv.SelectedIdx, _ = SliceIdxByValue(sv.Slice, sv.SelVal)
	}
	if sv.IsInactive() && sv.SelectedIdx >= 0 {
		sv.SelectIdxWidgets(sv.SelectedIdx, true)
	}
	sv.UpdateScroll()
}

// SetChanged sets the Changed flag and emits the ViewSig signal for the
// SliceView, indicating that some kind of edit / change has taken place to
// the table data.  It isn't really practical to record all the different
// types of changes, so this is just generic.
func (sv *SliceView) SetChanged() {
	sv.Changed = true
	sv.ViewSig.Emit(sv.This(), 0, nil)
	sv.ToolBar().UpdateActions() // nil safe
}

// SliceNewAtRow inserts a new blank element at given display row
func (sv *SliceView) SliceNewAtRow(row int, reconfig bool) {
	sv.SliceNewAt(sv.StartIdx+row, reconfig)
}

// SliceNewAt inserts a new blank element at given index in the slice -- -1
// means the end
func (sv *SliceView) SliceNewAt(idx int, reconfig bool) {
	if sv.isArray {
		return
	}

	updt := sv.UpdateStart()
	defer sv.UpdateEnd(updt)

	sltyp := kit.SliceElType(sv.Slice) // has pointer if it is there
	iski := ki.IsKi(sltyp)
	slptr := sltyp.Kind() == reflect.Ptr

	svl := reflect.ValueOf(sv.Slice)
	svnp, sz := sv.SliceValueSize()

	if iski && sv.SliceValView != nil {
		vvb := sv.SliceValView.AsValueViewBase()
		if vvb.Owner != nil {
			if ownki, ok := vvb.Owner.(ki.Ki); ok {
				gi.NewKiDialog(sv.Viewport, ownki.BaseIface(),
					gi.DlgOpts{Title: "Slice New", Prompt: "Number and Type of Items to Insert:"},
					sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
						if sig == int64(gi.DialogAccepted) {
							// svv, _ := recv.Embed(KiT_SliceView).(*SliceView)
							dlg, _ := send.(*gi.Dialog)
							n, typ := gi.NewKiDialogValues(dlg)
							updt := ownki.UpdateStart()
							for i := 0; i < n; i++ {
								nm := fmt.Sprintf("New%v%v", typ.Name(), idx+1+i)
								ownki.InsertNewChild(typ, idx+1+i, nm)
							}
							sv.SetChanged()
							ownki.UpdateEnd(updt)
						}
					})
			}
		}
	} else {
		nval := reflect.New(kit.NonPtrType(sltyp)) // make the concrete el
		if !slptr {
			nval = nval.Elem() // use concrete value
		}
		svnp = reflect.Append(svnp, nval)
		if idx >= 0 && idx < sz {
			reflect.Copy(svnp.Slice(idx+1, sz+1), svnp.Slice(idx, sz))
			svnp.Index(idx).Set(nval)
		}
		svl.Elem().Set(svnp)
	}

	if sv.TmpSave != nil {
		sv.TmpSave.SaveTmp()
	}
	sv.SetChanged()
	if reconfig {
		sv.LayoutSliceGrid()
		sv.UpdateSliceGrid()
	}
}

// SliceDeleteAtRow deletes element at given display row
func (sv *SliceView) SliceDeleteAtRow(row int, reconfig bool) {
	sv.SliceDeleteAt(sv.StartIdx+row, reconfig)
}

// SliceDeleteAt deletes element at given index from slice
func (sv *SliceView) SliceDeleteAt(idx int, reconfig bool) {
	if sv.isArray {
		return
	}

	updt := sv.UpdateStart()
	defer sv.UpdateEnd(updt)

	kit.SliceDeleteAt(sv.Slice, idx)

	if sv.TmpSave != nil {
		sv.TmpSave.SaveTmp()
	}
	sv.SetChanged()
	if reconfig {
		sv.LayoutSliceGrid()
		sv.UpdateSliceGrid()
	}
}

// ConfigToolbar configures the toolbar actions
func (sv *SliceView) ConfigToolbar() {
	if kit.IfaceIsNil(sv.Slice) || sv.IsInactive() {
		return
	}
	if sv.ToolbarSlice == sv.Slice {
		return
	}
	tb := sv.ToolBar()
	nact := 1
	if sv.isArray || sv.IsInactive() {
		nact = 0
	}
	if len(*tb.Children()) < nact {
		tb.SetStretchMaxWidth()
		if !sv.isArray && !sv.DeleteOnly {
			tb.AddAction(gi.ActOpts{Label: "Add", Icon: "plus"},
				sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
					svv := recv.Embed(KiT_SliceView).(*SliceView)
					svv.SliceNewAt(-1, true)
				})
		}
	}
	sz := len(*tb.Children())
	if sz > nact {
		for i := sz - 1; i >= nact; i-- {
			tb.DeleteChildAtIndex(i, true)
		}
	}
	if HasToolBarView(sv.Slice) {
		ToolBarView(sv.Slice, sv.Viewport, tb)
	}
	sv.ToolbarSlice = sv.Slice
}

func (sv *SliceView) Style2D() {
	if !sv.IsConfiged() {
		return
	}
	if sv.IsInactive() {
		sv.SetCanFocus()
	}
	sg := sv.SliceGrid()
	sg.StartFocus() // need to call this when window is actually active
	sv.Frame.Style2D()
}

func (sv *SliceView) Render2D() {
	sv.ToolBar().UpdateActions()
	if win := sv.ParentWindow(); win != nil {
		if !win.IsResizing() {
			win.MainMenuUpdateActives()
		}
	}
	if sv.SliceGridNeedsLayout() {
		// note: we are outside of slice grid and thus cannot do proper layout during Layout2D
		// as we don't yet know the size of grid -- so we catch it here at next step and just
		// rebuild as needed.
		sv.renderedRows = sv.DispRows
		if sv.LayoutSliceGrid() {
			sv.UpdateSliceGrid()
		}
		sv.ReRender2DTree()
		if sv.SelectedIdx > -1 {
			sv.ScrollToIdx(sv.SelectedIdx)
		}
		return
	}
	if sv.FullReRenderIfNeeded() {
		return
	}
	if sv.PushBounds() {
		sv.FrameStdRender()
		sv.This().(gi.Node2D).ConnectEvents2D()
		sv.RenderScrolls()
		sv.Render2DChildren()
		sv.PopBounds()
	} else {
		sv.DisconnectAllEvents(gi.AllPris)
	}
}

func (sv *SliceView) ConnectEvents2D() {
	sv.SliceViewEvents()
}

func (sv *SliceView) HasFocus2D() bool {
	if sv.IsInactive() {
		return sv.InactKeyNav
	}
	return sv.ContainsFocus() // anyone within us gives us focus..
}

//////////////////////////////////////////////////////////////////////////////
//  Row access methods
//  NOTE: row = physical GUI display row, idx = slice index -- not the same!

// SliceVal returns value interface at given slice index
func (sv *SliceView) SliceVal(idx int) interface{} {
	svnp, sz := sv.SliceValueSize()
	if idx < 0 || idx >= sz {
		fmt.Printf("giv.SliceView: slice index out of range: %v\n", idx)
		return nil
	}
	val := kit.OnePtrUnderlyingValue(svnp.Index(idx)) // deal with pointer lists
	vali := val.Interface()
	return vali
}

// IsRowInBounds returns true if disp row is in bounds
func (sv *SliceView) IsRowInBounds(row int) bool {
	return row >= 0 && row < sv.DispRows
}

// IsIdxVisible returns true if slice index is currently visible
func (sv *SliceView) IsIdxVisible(idx int) bool {
	return sv.IsRowInBounds(idx - sv.StartIdx)
}

// RowFirstWidget returns the first widget for given row (could be index or
// not) -- false if out of range
func (sv *SliceView) RowFirstWidget(row int) (*gi.WidgetBase, bool) {
	if !sv.ShowIndex {
		return nil, false
	}
	if !sv.IsRowInBounds(row) {
		return nil, false
	}
	nWidgPerRow, _ := sv.RowWidgetNs()
	sg := sv.SliceGrid()
	widg := sg.Kids[row*nWidgPerRow].(gi.Node2D).AsWidget()
	return widg, true
}

// RowGrabFocus grabs the focus for the first focusable widget in given row --
// returns that element or nil if not successful -- note: grid must have
// already rendered for focus to be grabbed!
func (sv *SliceView) RowGrabFocus(row int) *gi.WidgetBase {
	if !sv.IsRowInBounds(row) || sv.inFocusGrab { // range check
		return nil
	}
	nWidgPerRow, idxOff := sv.RowWidgetNs()
	sg := sv.SliceGrid()
	ridx := nWidgPerRow * row
	widg := sg.Child(ridx + idxOff).(gi.Node2D).AsWidget()
	if widg.HasFocus() {
		return widg
	}
	sv.inFocusGrab = true
	widg.GrabFocus()
	sv.inFocusGrab = false
	return widg
}

// IdxGrabFocus grabs the focus for the first focusable widget in given idx --
// returns that element or nil if not successful
func (sv *SliceView) IdxGrabFocus(idx int) *gi.WidgetBase {
	sv.ScrollToIdx(idx)
	return sv.RowGrabFocus(idx - sv.StartIdx)
}

// IdxPos returns center of window position of index label for idx (ContextMenuPos)
func (sv *SliceView) IdxPos(idx int) image.Point {
	row := idx - sv.StartIdx
	if row < 0 {
		row = 0
	}
	if row > sv.DispRows-1 {
		row = sv.DispRows - 1
	}
	var pos image.Point
	widg, ok := sv.RowFirstWidget(row)
	if ok {
		pos = widg.ContextMenuPos()
	}
	return pos
}

// RowFromPos returns the row that contains given vertical position, false if not found
func (sv *SliceView) RowFromPos(posY int) (int, bool) {
	// todo: could optimize search to approx loc, and search up / down from there
	for rw := 0; rw < sv.DispRows; rw++ {
		widg, ok := sv.RowFirstWidget(rw)
		if ok {
			if widg.WinBBox.Min.Y < posY && posY < widg.WinBBox.Max.Y {
				return rw, true
			}
		}
	}
	return -1, false
}

// ScrollToIdx ensures that given slice idx is visible by scrolling display as needed
func (sv *SliceView) ScrollToIdx(idx int) bool {
	if idx < sv.StartIdx {
		sv.StartIdx = idx
		sv.StartIdx = ints.MaxInt(0, sv.StartIdx)
		sv.UpdateScroll()
		sv.UpdateSliceGrid()
		return true
	} else if idx >= sv.StartIdx+sv.DispRows {
		sv.StartIdx = idx - (sv.DispRows - 1)
		sv.StartIdx = ints.MaxInt(0, sv.StartIdx)
		sv.UpdateScroll()
		sv.UpdateSliceGrid()
		return true
	}
	return false
}

// SelectVal sets SelVal and attempts to find corresponding row, setting
// SelectedIdx and selecting row if found -- returns true if found, false
// otherwise.
func (sv *SliceView) SelectVal(val string) bool {
	sv.SelVal = val
	if sv.SelVal != nil {
		idx, _ := SliceIdxByValue(sv.Slice, sv.SelVal)
		if idx >= 0 {
			sv.ScrollToIdx(idx)
			sv.UpdateSelectIdx(idx, true)
			return true
		}
	}
	return false
}

// SliceIdxByValue searches for first index that contains given value in slice
// -- returns false if not found
func SliceIdxByValue(slc interface{}, fldVal interface{}) (int, bool) {
	svnp := kit.NonPtrValue(reflect.ValueOf(slc))
	sz := svnp.Len()
	for row := 0; row < sz; row++ {
		rval := kit.NonPtrValue(svnp.Index(row))
		if rval.Interface() == fldVal {
			return row, true
		}
	}
	return -1, false
}

/////////////////////////////////////////////////////////////////////////////
//    Moving

// MoveDown moves the selection down to next row, using given select mode
// (from keyboard modifiers) -- returns newly selected row or -1 if failed
func (sv *SliceView) MoveDown(selMode mouse.SelectModes) int {
	if sv.SelectedIdx >= sv.SliceSize-1 {
		sv.SelectedIdx = sv.SliceSize - 1
		return -1
	}
	sv.SelectedIdx++
	sv.SelectIdxAction(sv.SelectedIdx, selMode)
	return sv.SelectedIdx
}

// MoveDownAction moves the selection down to next row, using given select
// mode (from keyboard modifiers) -- and emits select event for newly selected
// row
func (sv *SliceView) MoveDownAction(selMode mouse.SelectModes) int {
	nidx := sv.MoveDown(selMode)
	if nidx >= 0 {
		sv.ScrollToIdx(nidx)
		sv.WidgetSig.Emit(sv.This(), int64(gi.WidgetSelected), nidx)
	}
	return nidx
}

// MoveUp moves the selection up to previous idx, using given select mode
// (from keyboard modifiers) -- returns newly selected idx or -1 if failed
func (sv *SliceView) MoveUp(selMode mouse.SelectModes) int {
	if sv.SelectedIdx <= 0 {
		sv.SelectedIdx = 0
		return -1
	}
	sv.SelectedIdx--
	sv.SelectIdxAction(sv.SelectedIdx, selMode)
	return sv.SelectedIdx
}

// MoveUpAction moves the selection up to previous idx, using given select
// mode (from keyboard modifiers) -- and emits select event for newly selected idx
func (sv *SliceView) MoveUpAction(selMode mouse.SelectModes) int {
	nidx := sv.MoveUp(selMode)
	if nidx >= 0 {
		sv.ScrollToIdx(nidx)
		sv.WidgetSig.Emit(sv.This(), int64(gi.WidgetSelected), nidx)
	}
	return nidx
}

// MovePageDown moves the selection down to next page, using given select mode
// (from keyboard modifiers) -- returns newly selected idx or -1 if failed
func (sv *SliceView) MovePageDown(selMode mouse.SelectModes) int {
	if sv.SelectedIdx >= sv.SliceSize-1 {
		sv.SelectedIdx = sv.SliceSize - 1
		return -1
	}
	sv.SelectedIdx += sv.VisRows
	sv.SelectedIdx = ints.MinInt(sv.SelectedIdx, sv.SliceSize-1)
	sv.SelectIdxAction(sv.SelectedIdx, selMode)
	return sv.SelectedIdx
}

// MovePageDownAction moves the selection down to next page, using given select
// mode (from keyboard modifiers) -- and emits select event for newly selected idx
func (sv *SliceView) MovePageDownAction(selMode mouse.SelectModes) int {
	nidx := sv.MovePageDown(selMode)
	if nidx >= 0 {
		sv.ScrollToIdx(nidx)
		sv.WidgetSig.Emit(sv.This(), int64(gi.WidgetSelected), nidx)
	}
	return nidx
}

// MovePageUp moves the selection up to previous page, using given select mode
// (from keyboard modifiers) -- returns newly selected idx or -1 if failed
func (sv *SliceView) MovePageUp(selMode mouse.SelectModes) int {
	if sv.SelectedIdx <= 0 {
		sv.SelectedIdx = 0
		return -1
	}
	sv.SelectedIdx -= sv.VisRows
	sv.SelectedIdx = ints.MaxInt(0, sv.SelectedIdx)
	sv.SelectIdxAction(sv.SelectedIdx, selMode)
	return sv.SelectedIdx
}

// MovePageUpAction moves the selection up to previous page, using given select
// mode (from keyboard modifiers) -- and emits select event for newly selected idx
func (sv *SliceView) MovePageUpAction(selMode mouse.SelectModes) int {
	nidx := sv.MovePageUp(selMode)
	if nidx >= 0 {
		sv.ScrollToIdx(nidx)
		sv.WidgetSig.Emit(sv.This(), int64(gi.WidgetSelected), nidx)
	}
	return nidx
}

//////////////////////////////////////////////////////////////////////////////
//    Selection: user operates on the index labels

// SelectRowWidgets sets the selection state of given row of widgets
func (sv *SliceView) SelectRowWidgets(row int, sel bool) {
	sg := sv.SliceGrid()
	nWidgPerRow, idxOff := sv.RowWidgetNs()
	rowidx := row * nWidgPerRow
	if sv.ShowIndex {
		if sg.Kids.IsValidIndex(rowidx) == nil {
			widg := sg.Child(rowidx).(gi.Node2D).AsNode2D()
			widg.SetSelectedState(sel)
			widg.UpdateSig()
		}
	}
	if sg.Kids.IsValidIndex(rowidx+idxOff) == nil {
		widg := sg.Child(rowidx + idxOff).(gi.Node2D).AsNode2D()
		widg.SetSelectedState(sel)
		widg.UpdateSig()
	}
}

// SelectIdxWidgets sets the selection state of given slice index
// returns false if index is not visible
func (sv *SliceView) SelectIdxWidgets(idx int, sel bool) bool {
	if !sv.IsIdxVisible(idx) {
		return false
	}
	sv.SelectRowWidgets(idx-sv.StartIdx, sel)
	return true
}

// UpdateSelectRow updates the selection for the given row
// callback from widgetsig select
func (sv *SliceView) UpdateSelectRow(row int, sel bool) {
	idx := row + sv.StartIdx
	sv.UpdateSelectIdx(idx, sel)
}

// UpdateSelectRow updates the selection for the given index
func (sv *SliceView) UpdateSelectIdx(idx int, sel bool) {
	if sv.IsInactive() {
		if sv.SelectedIdx == idx { // never unselect
			sv.SelectIdxWidgets(sv.SelectedIdx, true)
			return
		}
		if sv.SelectedIdx >= 0 { // unselect current
			sv.SelectIdxWidgets(sv.SelectedIdx, false)
		}
		if sel {
			sv.SelectedIdx = idx
			sv.SelectIdxWidgets(sv.SelectedIdx, true)
		}
		sv.WidgetSig.Emit(sv.This(), int64(gi.WidgetSelected), sv.SelectedIdx)
	} else {
		selMode := mouse.SelectOne
		win := sv.Viewport.Win
		if win != nil {
			selMode = win.LastSelMode
		}
		sv.SelectIdxAction(idx, selMode)
	}
}

// IdxIsSelected returns the selected status of given slice index
func (sv *SliceView) IdxIsSelected(idx int) bool {
	if _, ok := sv.SelectedIdxs[idx]; ok {
		return true
	}
	return false
}

// SelectedIdxsList returns list of selected indexes, sorted either ascending or descending
func (sv *SliceView) SelectedIdxsList(descendingSort bool) []int {
	rws := make([]int, len(sv.SelectedIdxs))
	i := 0
	for r, _ := range sv.SelectedIdxs {
		rws[i] = r
		i++
	}
	if descendingSort {
		sort.Slice(rws, func(i, j int) bool {
			return rws[i] > rws[j]
		})
	} else {
		sort.Slice(rws, func(i, j int) bool {
			return rws[i] < rws[j]
		})
	}
	return rws
}

// SelectIdx selects given idx (if not already selected) -- updates select
// status of index label
func (sv *SliceView) SelectIdx(idx int) {
	sv.SelectedIdxs[idx] = struct{}{}
	sv.SelectIdxWidgets(idx, true)
}

// UnselectIdx unselects given idx (if selected)
func (sv *SliceView) UnselectIdx(idx int) {
	if sv.IdxIsSelected(idx) {
		delete(sv.SelectedIdxs, idx)
	}
	sv.SelectIdxWidgets(idx, false)
}

// UnselectAllIdxs unselects all selected idxs
func (sv *SliceView) UnselectAllIdxs() {
	win := sv.Viewport.Win
	updt := false
	if win != nil {
		updt = win.UpdateStart()
	}
	for r, _ := range sv.SelectedIdxs {
		sv.SelectIdxWidgets(r, false)
	}
	sv.SelectedIdxs = make(map[int]struct{})
	if win != nil {
		win.UpdateEnd(updt)
	}
}

// SelectAllIdxs selects all idxs
func (sv *SliceView) SelectAllIdxs() {
	win := sv.Viewport.Win
	updt := false
	if win != nil {
		updt = win.UpdateStart()
	}
	sv.UnselectAllIdxs()
	sv.SelectedIdxs = make(map[int]struct{}, sv.SliceSize)
	for idx := 0; idx < sv.SliceSize; idx++ {
		sv.SelectedIdxs[idx] = struct{}{}
		sv.SelectIdxWidgets(idx, true)
	}
	if win != nil {
		win.UpdateEnd(updt)
	}
}

// SelectIdxAction is called when a select action has been received (e.g., a
// mouse click) -- translates into selection updates -- gets selection mode
// from mouse event (ExtendContinuous, ExtendOne)
func (sv *SliceView) SelectIdxAction(idx int, mode mouse.SelectModes) {
	if mode == mouse.NoSelect {
		return
	}
	idx = ints.MinInt(idx, sv.SliceSize-1)
	if idx < 0 {
		idx = 0
	}
	// row := idx - sv.StartIdx // note: could be out of bounds
	win := sv.Viewport.Win
	updt := false
	if win != nil {
		updt = win.UpdateStart()
	}
	switch mode {
	case mouse.SelectOne:
		if sv.IdxIsSelected(idx) {
			if len(sv.SelectedIdxs) > 1 {
				sv.UnselectAllIdxs()
			}
			sv.SelectedIdx = idx
			sv.SelectIdx(idx)
			sv.IdxGrabFocus(idx)
		} else {
			sv.UnselectAllIdxs()
			sv.SelectedIdx = idx
			sv.SelectIdx(idx)
			sv.IdxGrabFocus(idx)
		}
		sv.WidgetSig.Emit(sv.This(), int64(gi.WidgetSelected), sv.SelectedIdx)
	case mouse.ExtendContinuous:
		if len(sv.SelectedIdxs) == 0 {
			sv.SelectedIdx = idx
			sv.SelectIdx(idx)
			sv.IdxGrabFocus(idx)
			sv.WidgetSig.Emit(sv.This(), int64(gi.WidgetSelected), sv.SelectedIdx)
		} else {
			minIdx := -1
			maxIdx := 0
			for r, _ := range sv.SelectedIdxs {
				if minIdx < 0 {
					minIdx = r
				} else {
					minIdx = ints.MinInt(minIdx, r)
				}
				maxIdx = ints.MaxInt(maxIdx, r)
			}
			cidx := idx
			sv.SelectedIdx = idx
			sv.SelectIdx(idx)
			if idx < minIdx {
				for cidx < minIdx {
					r := sv.MoveDown(mouse.SelectQuiet) // just select
					cidx = r
				}
			} else if idx > maxIdx {
				for cidx > maxIdx {
					r := sv.MoveUp(mouse.SelectQuiet) // just select
					cidx = r
				}
			}
			sv.IdxGrabFocus(idx)
			sv.WidgetSig.Emit(sv.This(), int64(gi.WidgetSelected), sv.SelectedIdx)
		}
	case mouse.ExtendOne:
		if sv.IdxIsSelected(idx) {
			sv.UnselectIdxAction(idx)
		} else {
			sv.SelectedIdx = idx
			sv.SelectIdx(idx)
			sv.IdxGrabFocus(idx)
			sv.WidgetSig.Emit(sv.This(), int64(gi.WidgetSelected), sv.SelectedIdx)
		}
	case mouse.Unselect:
		sv.SelectedIdx = idx
		sv.UnselectIdxAction(idx)
	case mouse.SelectQuiet:
		sv.SelectedIdx = idx
		sv.SelectIdx(idx)
	case mouse.UnselectQuiet:
		sv.SelectedIdx = idx
		sv.UnselectIdx(idx)
	}
	if win != nil {
		win.UpdateEnd(updt)
	}
}

// UnselectIdxAction unselects this idx (if selected) -- and emits a signal
func (sv *SliceView) UnselectIdxAction(idx int) {
	if sv.IdxIsSelected(idx) {
		sv.UnselectIdx(idx)
	}
}

//////////////////////////////////////////////////////////////////////////////
//    Copy / Cut / Paste

// MimeDataRow adds mimedata for given idx: an application/json of the struct
func (sv *SliceView) MimeDataIdx(md *mimedata.Mimes, idx int) {
	val := sv.SliceVal(idx)
	b, err := json.MarshalIndent(val, "", "  ")
	if err == nil {
		*md = append(*md, &mimedata.Data{Type: filecat.DataJson, Data: b})
	} else {
		log.Printf("gi.SliceView MimeData JSON Marshall error: %v\n", err)
	}
}

// FromMimeData creates a slice of structs from mime data
func (sv *SliceView) FromMimeData(md mimedata.Mimes) []interface{} {
	svnp, _ := sv.SliceValueSize()
	svtyp := svnp.Type()
	sl := make([]interface{}, 0, len(md))
	for _, d := range md {
		if d.Type == filecat.DataJson {
			nval := reflect.New(svtyp.Elem()).Interface()
			err := json.Unmarshal(d.Data, nval)
			if err == nil {
				sl = append(sl, nval)
			} else {
				log.Printf("gi.SliceView FromMimeData: JSON load error: %v\n", err)
			}
		}
	}
	return sl
}

// Copy copies selected rows to clip.Board, optionally resetting the selection
// satisfies gi.Clipper interface and can be overridden by subtypes
func (sv *SliceView) Copy(reset bool) {
	nitms := len(sv.SelectedIdxs)
	if nitms == 0 {
		return
	}
	ixs := sv.SelectedIdxsList(false) // ascending
	md := make(mimedata.Mimes, 0, nitms)
	for _, i := range ixs {
		sv.MimeDataIdx(&md, i)
	}
	oswin.TheApp.ClipBoard(sv.Viewport.Win.OSWin).Write(md)
	if reset {
		sv.UnselectAllIdxs()
	}
}

// CopyIdxs copies selected idxs to clip.Board, optionally resetting the selection
func (sv *SliceView) CopyIdxs(reset bool) {
	if cpr, ok := sv.This().(gi.Clipper); ok { // should always be true, but justin case..
		cpr.Copy(reset)
	} else {
		sv.Copy(reset)
	}
}

// DeleteIdxs deletes all selected indexes
func (sv *SliceView) DeleteIdxs() {
	if len(sv.SelectedIdxs) == 0 {
		return
	}
	updt := sv.UpdateStart()
	ixs := sv.SelectedIdxsList(true) // descending sort
	for _, r := range ixs {
		sv.SliceDeleteAt(r, false)
	}
	sv.SetChanged()
	sv.UpdateSliceGrid()
	sv.UpdateEnd(updt)
}

// Cut copies selected rows to clip.Board and deletes selected rows
// satisfies gi.Clipper interface and can be overridden by subtypes
func (sv *SliceView) Cut() {
	if len(sv.SelectedIdxs) == 0 {
		return
	}
	updt := sv.UpdateStart()
	sv.CopyIdxs(false)
	ixs := sv.SelectedIdxsList(true) // descending sort
	idx := ixs[0]
	sv.UnselectAllIdxs()
	for _, i := range ixs {
		sv.SliceDeleteAt(i, false)
	}
	sv.SetChanged()
	sv.UpdateSliceGrid()
	sv.UpdateEnd(updt)
	sv.SelectIdxAction(idx, mouse.SelectOne)
}

// CutIdxs copies selected rows to clip.Board and deletes selected rows
func (sv *SliceView) CutIdxs() {
	if cpr, ok := sv.This().(gi.Clipper); ok { // should always be true, but justin case..
		cpr.Cut()
	} else {
		sv.Cut()
	}
}

// Paste pastes clipboard at given row
// satisfies gi.Clipper interface and can be overridden by subtypes
func (sv *SliceView) Paste() {
	md := oswin.TheApp.ClipBoard(sv.Viewport.Win.OSWin).Read([]string{filecat.DataJson})
	if md != nil {
		sv.PasteMenu(md, sv.curIdx)
	}
}

// PasteIdx pastes clipboard at given idx
func (sv *SliceView) PasteIdx(idx int) {
	sv.curIdx = idx
	if cpr, ok := sv.This().(gi.Clipper); ok { // should always be true, but justin case..
		cpr.Paste()
	} else {
		sv.Paste()
	}
}

// MakePasteMenu makes the menu of options for paste events
func (sv *SliceView) MakePasteMenu(m *gi.Menu, data interface{}, idx int) {
	if len(*m) > 0 {
		return
	}
	m.AddAction(gi.ActOpts{Label: "Assign To", Data: data}, sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
		svv := recv.Embed(KiT_SliceView).(*SliceView)
		svv.PasteAssign(data.(mimedata.Mimes), idx)
	})
	m.AddAction(gi.ActOpts{Label: "Insert Before", Data: data}, sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
		svv := recv.Embed(KiT_SliceView).(*SliceView)
		svv.PasteAtIdx(data.(mimedata.Mimes), idx)
	})
	m.AddAction(gi.ActOpts{Label: "Insert After", Data: data}, sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
		svv := recv.Embed(KiT_SliceView).(*SliceView)
		svv.PasteAtIdx(data.(mimedata.Mimes), idx+1)
	})
	m.AddAction(gi.ActOpts{Label: "Cancel", Data: data}, sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
	})
}

// PasteMenu performs a paste from the clipboard using given data -- pops up
// a menu to determine what specifically to do
func (sv *SliceView) PasteMenu(md mimedata.Mimes, idx int) {
	sv.UnselectAllIdxs()
	var men gi.Menu
	sv.MakePasteMenu(&men, md, idx)
	pos := sv.IdxPos(idx)
	gi.PopupMenu(men, pos.X, pos.Y, sv.Viewport, "svPasteMenu")
}

// PasteAssign assigns mime data (only the first one!) to this row
func (sv *SliceView) PasteAssign(md mimedata.Mimes, row int) {
	svnp, _ := sv.SliceValueSize()

	sl := sv.FromMimeData(md)
	updt := sv.UpdateStart()
	if len(sl) == 0 {
		return
	}
	ns := sl[0]
	svnp.Index(row).Set(reflect.ValueOf(ns).Elem())
	if sv.TmpSave != nil {
		sv.TmpSave.SaveTmp()
	}
	sv.SetChanged()
	sv.UpdateSliceGrid()
	sv.UpdateEnd(updt)
}

// PasteAtIdx inserts object(s) from mime data at (before) given slice index
func (sv *SliceView) PasteAtIdx(md mimedata.Mimes, idx int) {
	svnp, _ := sv.SliceValueSize()
	svl := reflect.ValueOf(sv.Slice)

	sl := sv.FromMimeData(md)
	updt := sv.UpdateStart()
	for _, ns := range sl {
		sz := svnp.Len()
		svnp = reflect.Append(svnp, reflect.ValueOf(ns).Elem())
		svl.Elem().Set(svnp)
		if idx >= 0 && idx < sz {
			reflect.Copy(svnp.Slice(idx+1, sz+1), svnp.Slice(idx, sz))
			svnp.Index(idx).Set(reflect.ValueOf(ns).Elem())
			svl.Elem().Set(svnp)
		}
		idx++
	}
	if sv.TmpSave != nil {
		sv.TmpSave.SaveTmp()
	}
	sv.SetChanged()
	sv.UpdateSliceGrid()
	sv.UpdateEnd(updt)
	sv.SelectIdxAction(idx, mouse.SelectOne)
}

// Duplicate copies selected items and inserts them after current selection --
// return row of start of duplicates if successful, else -1
func (sv *SliceView) Duplicate() int {
	nitms := len(sv.SelectedIdxs)
	if nitms == 0 {
		return -1
	}
	ixs := sv.SelectedIdxsList(true) // descending sort -- last first
	pasteAt := ixs[0]
	sv.CopyIdxs(true)
	md := oswin.TheApp.ClipBoard(sv.Viewport.Win.OSWin).Read([]string{filecat.DataJson})
	sv.PasteAtIdx(md, pasteAt)
	return pasteAt
}

//////////////////////////////////////////////////////////////////////////////
//    Drag-n-Drop

// DragNDropStart starts a drag-n-drop
func (sv *SliceView) DragNDropStart() {
	nitms := len(sv.SelectedIdxs)
	if nitms == 0 {
		return
	}
	md := make(mimedata.Mimes, 0, nitms)
	for i, _ := range sv.SelectedIdxs {
		sv.MimeDataIdx(&md, i)
	}
	ixs := sv.SelectedIdxsList(true) // descending sort
	widg, ok := sv.RowFirstWidget(ixs[0])
	if ok {
		bi := &gi.Bitmap{}
		bi.InitName(bi, sv.UniqueName())
		bi.GrabRenderFrom(widg)
		gi.ImageClearer(bi.Pixels, 50.0)
		sv.Viewport.Win.StartDragNDrop(sv.This(), md, bi)
	}
}

// DragNDropTarget handles a drag-n-drop drop
func (sv *SliceView) DragNDropTarget(de *dnd.Event) {
	de.Target = sv.This()
	if de.Mod == dnd.DropLink {
		de.Mod = dnd.DropCopy // link not supported -- revert to copy
	}
	row, ok := sv.RowFromPos(de.Where.Y)
	if ok {
		de.SetProcessed()
		sv.curIdx = row
		if dpr, ok := sv.This().(gi.DragNDropper); ok {
			dpr.Drop(de.Data, de.Mod)
		} else {
			sv.Drop(de.Data, de.Mod)
		}
	}
}

// MakeDropMenu makes the menu of options for dropping on a target
func (sv *SliceView) MakeDropMenu(m *gi.Menu, data interface{}, mod dnd.DropMods, row int) {
	if len(*m) > 0 {
		return
	}
	switch mod {
	case dnd.DropCopy:
		m.AddLabel("Copy (Shift=Move):")
	case dnd.DropMove:
		m.AddLabel("Move:")
	}
	if mod == dnd.DropCopy {
		m.AddAction(gi.ActOpts{Label: "Assign To", Data: data}, sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
			svv := recv.Embed(KiT_SliceView).(*SliceView)
			svv.DropAssign(data.(mimedata.Mimes), row)
		})
	}
	m.AddAction(gi.ActOpts{Label: "Insert Before", Data: data}, sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
		svv := recv.Embed(KiT_SliceView).(*SliceView)
		svv.DropBefore(data.(mimedata.Mimes), mod, row) // captures mod
	})
	m.AddAction(gi.ActOpts{Label: "Insert After", Data: data}, sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
		svv := recv.Embed(KiT_SliceView).(*SliceView)
		svv.DropAfter(data.(mimedata.Mimes), mod, row) // captures mod
	})
	m.AddAction(gi.ActOpts{Label: "Cancel", Data: data}, sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
		svv := recv.Embed(KiT_SliceView).(*SliceView)
		svv.DropCancel()
	})
}

// Drop pops up a menu to determine what specifically to do with dropped items
// this satisfies gi.DragNDropper interface, and can be overwritten in subtypes
func (sv *SliceView) Drop(md mimedata.Mimes, mod dnd.DropMods) {
	var men gi.Menu
	sv.MakeDropMenu(&men, md, mod, sv.curIdx)
	pos := sv.IdxPos(sv.curIdx)
	gi.PopupMenu(men, pos.X, pos.Y, sv.Viewport, "svDropMenu")
}

// DropAssign assigns mime data (only the first one!) to this node
func (sv *SliceView) DropAssign(md mimedata.Mimes, row int) {
	sv.DraggedIdxs = nil
	sv.PasteAssign(md, row)
	sv.DragNDropFinalize(dnd.DropCopy)
}

// DragNDropFinalize is called to finalize actions on the Source node prior to
// performing target actions -- mod must indicate actual action taken by the
// target, including ignore -- ends up calling DragNDropSource if us..
func (sv *SliceView) DragNDropFinalize(mod dnd.DropMods) {
	sv.UnselectAllIdxs()
	sv.Viewport.Win.FinalizeDragNDrop(mod)
}

// DragNDropSource is called after target accepts the drop -- we just remove
// elements that were moved
func (sv *SliceView) DragNDropSource(de *dnd.Event) {
	if de.Mod != dnd.DropMove || len(sv.DraggedIdxs) == 0 {
		return
	}
	updt := sv.UpdateStart()
	sort.Slice(sv.DraggedIdxs, func(i, j int) bool {
		return sv.DraggedIdxs[i] > sv.DraggedIdxs[j]
	})
	row := sv.DraggedIdxs[0]
	for _, r := range sv.DraggedIdxs {
		sv.SliceDeleteAt(r, false)
	}
	sv.DraggedIdxs = nil
	sv.UpdateSliceGrid()
	sv.UpdateEnd(updt)
	sv.SelectIdxAction(row, mouse.SelectOne)
}

// SaveDraggedIdxs saves selectedrows into dragged rows taking into account insertion at rows
func (sv *SliceView) SaveDraggedIdxs(row int) {
	sz := len(sv.SelectedIdxs)
	if sz == 0 {
		sv.DraggedIdxs = nil
		return
	}
	sv.DraggedIdxs = make([]int, len(sv.SelectedIdxs))
	idx := 0
	for r, _ := range sv.SelectedIdxs {
		if r > row {
			sv.DraggedIdxs[idx] = r + sz // make room for insertion
		} else {
			sv.DraggedIdxs[idx] = r
		}
		idx++
	}
}

// DropBefore inserts object(s) from mime data before this node
func (sv *SliceView) DropBefore(md mimedata.Mimes, mod dnd.DropMods, row int) {
	sv.SaveDraggedIdxs(row)
	sv.PasteAtIdx(md, row)
	sv.DragNDropFinalize(mod)
}

// DropAfter inserts object(s) from mime data after this node
func (sv *SliceView) DropAfter(md mimedata.Mimes, mod dnd.DropMods, row int) {
	sv.SaveDraggedIdxs(row + 1)
	sv.PasteAtIdx(md, row+1)
	sv.DragNDropFinalize(mod)
}

// DropCancel cancels the drop action e.g., preventing deleting of source
// items in a Move case
func (sv *SliceView) DropCancel() {
	sv.DragNDropFinalize(dnd.DropIgnore)
}

//////////////////////////////////////////////////////////////////////////////
//    Events

func (sv *SliceView) StdCtxtMenu(m *gi.Menu, row int) {
	if sv.isArray {
		return
	}
	m.AddAction(gi.ActOpts{Label: "Copy", Data: row},
		sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
			svv := recv.Embed(KiT_SliceView).(*SliceView)
			svv.CopyIdxs(true)
		})
	m.AddAction(gi.ActOpts{Label: "Cut", Data: row},
		sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
			svv := recv.Embed(KiT_SliceView).(*SliceView)
			svv.CutIdxs()
		})
	m.AddAction(gi.ActOpts{Label: "Paste", Data: row},
		sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
			svv := recv.Embed(KiT_SliceView).(*SliceView)
			svv.PasteIdx(data.(int))
		})
	m.AddAction(gi.ActOpts{Label: "Duplicate", Data: row},
		sv.This(), func(recv, send ki.Ki, sig int64, data interface{}) {
			svv := recv.Embed(KiT_SliceView).(*SliceView)
			svv.Duplicate()
		})
}

func (sv *SliceView) ItemCtxtMenu(idx int) {
	val := sv.SliceVal(idx)
	if val == nil {
		return
	}
	var men gi.Menu

	if CtxtMenuView(val, sv.IsInactive(), sv.Viewport, &men) {
		if sv.ShowViewCtxtMenu {
			men.AddSeparator("sep-svmenu")
			sv.StdCtxtMenu(&men, idx)
		}
	} else {
		sv.StdCtxtMenu(&men, idx)
	}
	if len(men) > 0 {
		pos := sv.IdxPos(idx)
		gi.PopupMenu(men, pos.X, pos.Y, sv.Viewport, sv.Nm+"-menu")
	}
}

func (sv *SliceView) KeyInputActive(kt *key.ChordEvent) {
	if gi.KeyEventTrace {
		fmt.Printf("SliceView KeyInput: %v\n", sv.PathUnique())
	}
	kf := gi.KeyFun(kt.Chord())
	selMode := mouse.SelectModeBits(kt.Modifiers)
	if selMode == mouse.SelectOne {
		if sv.SelectMode {
			selMode = mouse.ExtendContinuous
		}
	}
	idx := sv.SelectedIdx
	switch kf {
	case gi.KeyFunCancelSelect:
		sv.UnselectAllIdxs()
		sv.SelectMode = false
		kt.SetProcessed()
	case gi.KeyFunMoveDown:
		sv.MoveDownAction(selMode)
		kt.SetProcessed()
	case gi.KeyFunMoveUp:
		sv.MoveUpAction(selMode)
		kt.SetProcessed()
	case gi.KeyFunPageDown:
		sv.MovePageDownAction(selMode)
		kt.SetProcessed()
	case gi.KeyFunPageUp:
		sv.MovePageUpAction(selMode)
		kt.SetProcessed()
	case gi.KeyFunSelectMode:
		sv.SelectMode = !sv.SelectMode
		kt.SetProcessed()
	case gi.KeyFunSelectAll:
		sv.SelectAllIdxs()
		sv.SelectMode = false
		kt.SetProcessed()
	// case gi.KeyFunDelete: // too dangerous
	// 	sv.SliceDeleteAt(sv.SelectedIdx, true)
	// 	sv.SelectMode = false
	// 	sv.SelectIdxAction(idx, mouse.SelectOne)
	// 	kt.SetProcessed()
	case gi.KeyFunDuplicate:
		nidx := sv.Duplicate()
		sv.SelectMode = false
		if nidx >= 0 {
			sv.SelectIdxAction(nidx, mouse.SelectOne)
		}
		kt.SetProcessed()
	case gi.KeyFunInsert:
		sv.SliceNewAt(idx, true)
		sv.SelectMode = false
		sv.SelectIdxAction(idx+1, mouse.SelectOne) // todo: somehow nidx not working
		kt.SetProcessed()
	case gi.KeyFunInsertAfter:
		sv.SliceNewAt(idx+1, true)
		sv.SelectMode = false
		sv.SelectIdxAction(idx+1, mouse.SelectOne)
		kt.SetProcessed()
	case gi.KeyFunCopy:
		sv.CopyIdxs(true)
		sv.SelectMode = false
		sv.SelectIdxAction(idx, mouse.SelectOne)
		kt.SetProcessed()
	case gi.KeyFunCut:
		sv.CutIdxs()
		sv.SelectMode = false
		kt.SetProcessed()
	case gi.KeyFunPaste:
		sv.PasteIdx(sv.SelectedIdx)
		sv.SelectMode = false
		kt.SetProcessed()
	}
}

func (sv *SliceView) KeyInputInactive(kt *key.ChordEvent) {
	if gi.KeyEventTrace {
		fmt.Printf("SliceView Inactive KeyInput: %v\n", sv.PathUnique())
	}
	kf := gi.KeyFun(kt.Chord())
	idx := sv.SelectedIdx
	switch {
	case kf == gi.KeyFunMoveDown:
		ni := idx + 1
		if ni < sv.SliceSize {
			sv.ScrollToIdx(ni)
			sv.UpdateSelectIdx(ni, true)
			kt.SetProcessed()
		}
	case kf == gi.KeyFunMoveUp:
		ni := idx - 1
		if ni >= 0 {
			sv.ScrollToIdx(ni)
			sv.UpdateSelectIdx(ni, true)
			kt.SetProcessed()
		}
	case kf == gi.KeyFunPageDown:
		ni := ints.MinInt(idx+sv.VisRows-1, sv.SliceSize-1)
		sv.ScrollToIdx(ni)
		sv.UpdateSelectIdx(ni, true)
		kt.SetProcessed()
	case kf == gi.KeyFunPageUp:
		ni := ints.MaxInt(idx-(sv.VisRows-1), 0)
		sv.ScrollToIdx(ni)
		sv.UpdateSelectIdx(ni, true)
		kt.SetProcessed()
	case kf == gi.KeyFunEnter || kf == gi.KeyFunAccept || kt.Rune == ' ':
		sv.SliceViewSig.Emit(sv.This(), int64(SliceViewDoubleClicked), sv.SelectedIdx)
		kt.SetProcessed()
	}
}

func (sv *SliceView) SliceViewEvents() {
	if sv.IsInactive() {
		if sv.InactKeyNav {
			sv.ConnectEvent(oswin.KeyChordEvent, gi.RegPri, func(recv, send ki.Ki, sig int64, d interface{}) {
				svv := recv.Embed(KiT_SliceView).(*SliceView)
				kt := d.(*key.ChordEvent)
				svv.KeyInputInactive(kt)
			})
		}
		sv.ConnectEvent(oswin.MouseEvent, gi.LowRawPri, func(recv, send ki.Ki, sig int64, d interface{}) {
			me := d.(*mouse.Event)
			svv := recv.Embed(KiT_SliceView).(*SliceView)
			if !svv.HasFocus() {
				svv.GrabFocus()
			}
			if me.Button == mouse.Left && me.Action == mouse.DoubleClick {
				svv.SliceViewSig.Emit(svv.This(), int64(SliceViewDoubleClicked), svv.SelectedIdx)
				me.SetProcessed()
			}
			if me.Button == mouse.Right && me.Action == mouse.Release {
				svv.ItemCtxtMenu(svv.SelectedIdx)
				me.SetProcessed()
			}
		})
	} else {
		sv.ConnectEvent(oswin.MouseEvent, gi.LowRawPri, func(recv, send ki.Ki, sig int64, d interface{}) {
			me := d.(*mouse.Event)
			svv := recv.Embed(KiT_SliceView).(*SliceView)
			if me.Button == mouse.Right && me.Action == mouse.Release {
				svv.ItemCtxtMenu(svv.SelectedIdx)
				me.SetProcessed()
			}
		})
		sv.ConnectEvent(oswin.KeyChordEvent, gi.HiPri, func(recv, send ki.Ki, sig int64, d interface{}) {
			svv := recv.Embed(KiT_SliceView).(*SliceView)
			kt := d.(*key.ChordEvent)
			svv.KeyInputActive(kt)
		})
		sv.ConnectEvent(oswin.DNDEvent, gi.RegPri, func(recv, send ki.Ki, sig int64, d interface{}) {
			de := d.(*dnd.Event)
			svv := recv.Embed(KiT_SliceView).(*SliceView)
			switch de.Action {
			case dnd.Start:
				svv.DragNDropStart()
			case dnd.DropOnTarget:
				svv.DragNDropTarget(de)
			case dnd.DropFmSource:
				svv.DragNDropSource(de)
			}
		})
	}
}
