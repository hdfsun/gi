// Code generated by "stringer -type=MethViewFlags"; DO NOT EDIT.

package giv

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[MethViewConfirm-0]
	_ = x[MethViewShowReturn-1]
	_ = x[MethViewNoUpdateAfter-2]
	_ = x[MethViewHasSubMenu-3]
	_ = x[MethViewHasSubMenuVal-4]
	_ = x[MethViewKeyFun-5]
	_ = x[MethViewFlagsN-6]
}

const _MethViewFlags_name = "MethViewConfirmMethViewShowReturnMethViewNoUpdateAfterMethViewHasSubMenuMethViewHasSubMenuValMethViewKeyFunMethViewFlagsN"

var _MethViewFlags_index = [...]uint8{0, 15, 33, 54, 72, 93, 107, 121}

func (i MethViewFlags) String() string {
	if i < 0 || i >= MethViewFlags(len(_MethViewFlags_index)-1) {
		return "MethViewFlags(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _MethViewFlags_name[_MethViewFlags_index[i]:_MethViewFlags_index[i+1]]
}
