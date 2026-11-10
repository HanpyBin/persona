package extmgr

// Operating the user's iPhones for the assistant: a computer's node reaches the iPhones paired with it over
// RemotePairing (#3452, docs/extensions/iphone.md). The built-in extension iphone is off until the user turns it on,
// then computer by computer, and an iPhone is operated only once it consented from its own app (fail-closed: an iPhone that never said yes
// is never operated). While it is off no node is asked about iPhones, the assistant has no iphone_* tools and the
// machines carry no IPhone. Like the extensions cua and ego (computer.go, browser.go), whose switch it shares.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

const iphoneExtensionID = "iphone"

// iphoneSystems are the computers the device bridge runs on.
var iphoneSystems = []string{"darwin", "linux"}

// iphoneConfig is the extension's settings in l: the computers whose paired iPhones may be operated (each machine page's
// switch), the iPhones that consented (each from its own app) and whether every action is asked first.
func iphoneConfig(l i18n.Lang) json.RawMessage {
	t := func(key string) string { return i18n.T(l, "hub-iphone.manifest_"+key) }
	type field = map[string]any
	machines, _ := json.Marshal(field{"type": "array", "title": t("machines_title"), "description": t("machines_description"),
		"items": field{"type": "string", "format": "machine", "x-os": iphoneSystems}, "uniqueItems": true})
	phones, _ := json.Marshal(field{"type": "array", "title": t("phones_title"), "description": t("phones_description"),
		"items": field{"type": "string"}, "uniqueItems": true})
	confirm, _ := json.Marshal(field{"type": "boolean", "title": t("confirm_title"), "description": t("confirm_description"), "default": false})
	// Joined by hand to keep the properties in this order: the form follows it.
	return json.RawMessage(`{"type":"object","properties":{"machines":` + string(machines) + `,"phones":` + string(phones) +
		`,"confirm":` + string(confirm) + `}}`)
}

// iphoneManifest is the extension's manifest in English, with iphoneTranslations for the other languages.
func iphoneManifest(h *Manager) *extension.Manifest {
	items, _ := json.Marshal([]extension.ComputerItem{{ID: iphoneExtensionID, Name: "iPhone"}})
	return &extension.Manifest{ManifestVersion: extension.ManifestVersion, ID: iphoneExtensionID,
		Name: i18n.T(i18n.En, "hub-iphone.manifest_name"), Version: h.version, Description: i18n.T(i18n.En, "hub-iphone.manifest_description"),
		Contributes: map[string]json.RawMessage{"iphone": items}, Configuration: iphoneConfig(i18n.En), Node: builtinNode("machines")}
}

// iphoneTranslations are the manifest's name, description and settings in every language but English.
func iphoneTranslations() extension.Translations {
	out := extension.Translations{}
	for _, l := range []i18n.Lang{i18n.ZhHans, i18n.Ja} {
		t := func(key string) string { return i18n.T(l, "hub-iphone.manifest_"+key) }
		out[l] = &extension.Translation{Name: t("name"), Description: t("description"), Configuration: map[string]*extension.FieldText{
			"machines": {Title: t("machines_title"), Description: t("machines_description")},
			"phones":   {Title: t("phones_title"), Description: t("phones_description")},
			"confirm":  {Title: t("confirm_title"), Description: t("confirm_description")},
		}}
	}
	return out
}

// iphoneSettings is the extension's state: On when it is on, its computers, the iPhones that consented, whether to ask first.
type iphoneSettings struct {
	On       bool     `json:"-"`
	Machines []string `json:"machines"`
	Phones   []string `json:"phones"`
	Confirm  bool     `json:"confirm"`
}

func (h *Manager) iphone() iphoneSettings {
	if h.extensions == nil || h.extensions.reg == nil {
		return iphoneSettings{}
	}
	p := h.extensions.reg.Snapshot(iphoneExtensionID) // a copy, as machineExtension reads it
	if p == nil || !p.Record.Enabled {
		return iphoneSettings{}
	}
	s := iphoneSettings{On: true}
	_ = json.Unmarshal(h.extensions.reg.Config(p), &s)
	return s
}

// iphoneOf is m's proto.Machine.IPhone: nil unless the extension is on and m is a system the bridge runs on; else the
// last devices its node gave (every such computer's while it is on, so the iPhone's own app can find itself before
// the computer is turned on), with Enabled when it is on for m and each device's Allowed when also that iPhone consented.
func (h *Manager) iphoneOf(m *Machine) *proto.IPhone {
	s := h.iphone()
	m.mu.Lock()
	system, last := m.info.OS, m.iphone
	m.mu.Unlock()
	if !s.On || !slices.Contains(iphoneSystems, system) {
		return nil
	}
	p := proto.IPhone{}
	if last != nil {
		p = *last
		p.Devices = slices.Clone(last.Devices)
	}
	p.Enabled = slices.Contains(s.Machines, m.cfg.ID)
	for i := range p.Devices {
		p.Devices[i].Allowed = p.Enabled && p.Devices[i].ID != "" && slices.Contains(s.Phones, p.Devices[i].ID)
	}
	return &p
}

// refreshIPhone asks m's node (iphone.status) which iPhones it reaches now, while the extension is on and m is online; the
// machine list goes out again when that changed.
func (h *Manager) refreshIPhone(ctx context.Context, m *Machine) *proto.IPhone {
	if p := h.iphoneOf(m); p == nil || !m.isOnline() {
		return p
	}
	asked := time.Now()
	var p proto.IPhone
	if !m.hasIPhoneProgram() {
		p = proto.IPhone{Note: i18n.T(i18n.Default, "hub-iphone.note_outdated")}
	} else {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := m.callBuiltin(ctx, "iphone.status", map[string]any{}, &p); err != nil {
			return h.iphoneOf(m)
		}
	}
	p.Enabled = false // the manager's, never the node's word
	for i := range p.Devices {
		p.Devices[i].Allowed = false
	}
	if h.keepIPhone(m, p, asked) {
		h.broadcastMachines()
	}
	return h.iphoneOf(m)
}

// keepIPhone stores what m's node answered unless what is kept is newer (as keepComputer); true when it changed.
func (h *Manager) keepIPhone(m *Machine, p proto.IPhone, asked time.Time) bool {
	when := p.Checked
	if when.IsZero() {
		when = asked
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if when.Before(m.iphoneAt) {
		return false
	}
	old := m.iphone
	m.iphone, m.iphoneAt = &p, when
	if old == nil {
		return true
	}
	a, b := *old, p
	a.Checked, b.Checked = time.Time{}, time.Time{}
	return !reflect.DeepEqual(a, b)
}

// iphoneChanged: the extension was turned on or off, or its computers or iPhones changed: ask them again.
func (h *Manager) iphoneChanged(ctx context.Context) {
	for _, m := range h.machinesNow() {
		if p := h.iphoneOf(m); p != nil && m.isOnline() {
			go h.refreshIPhone(ctx, m)
		}
	}
	h.broadcastMachines()
}

// ---- the app's routes ----

// getIPhone: GET /api/v1/machines/{mid}/iphone, the iPhones this computer reaches now (its page asks when it opens).
// 404 while the extension is off or the bridge does not run there.
func (h *Manager) getIPhone(w http.ResponseWriter, r *http.Request, m *Machine) {
	p := h.refreshIPhone(r.Context(), m)
	if p == nil {
		writeErr(w, http.StatusNotFound, i18n.Err("hub-iphone.err_off"))
		return
	}
	writeJSON(w, i18n.IPhoneStatus(h.requestLang(r), p))
}

// putIPhone: PUT /api/v1/machines/{mid}/iphone {"enabled": bool}, the machine page's switch (the extension's machines).
func (h *Manager) putIPhone(w http.ResponseWriter, r *http.Request, m *Machine) {
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSONBody(w, r, &in, 4<<10); err != nil {
		h.writeComputerBodyErr(w, r, err)
		return
	}
	if in.Enabled == nil { // a body without it never turns the switch off
		writeErr(w, http.StatusBadRequest, i18n.Err("hub-iphone.err_no_enabled"))
		return
	}
	if h.iphoneOf(m) == nil {
		writeErr(w, http.StatusNotFound, i18n.Err("hub-iphone.err_off"))
		return
	}
	if err := h.setExtensionMachine(r.Context(), iphoneExtensionID, m.cfg.ID, *in.Enabled); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	h.logf("[%s] iphone control %v", m.cfg.ID, *in.Enabled)
	writeJSON(w, i18n.IPhoneStatus(h.requestLang(r), h.refreshIPhone(r.Context(), m)))
}

// postIPhoneJob: POST /api/v1/machines/{mid}/iphone/{pair|forget|cancel} {"device"}: the machine page's Pair (an
// iPhone plugged into this computer, trusted on the phone), Forget (its pairing record) and Cancel (no device). The node
// runs a pairing in the background; its state comes back as IPhone.job (the page asks again while it runs). Pairing
// needs the computer turned on; forgetting and cancelling only the extension on (a pairing started before the switch
// went off can still be stopped, a paired iPhone still forgotten).
func (h *Manager) postIPhoneJob(w http.ResponseWriter, r *http.Request, m *Machine) {
	action := r.PathValue("action")
	if action != "pair" && action != "forget" && action != "cancel" {
		writeErr(w, http.StatusNotFound, i18n.ErrEnglish("hub-computer.err_unknown_action"))
		return
	}
	var in struct {
		Device string `json:"device"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSONBody(w, r, &in, 4<<10); err != nil {
			h.writeComputerBodyErr(w, r, err)
			return
		}
	}
	in.Device = strings.TrimSpace(in.Device)
	if action != "cancel" && in.Device == "" {
		writeErr(w, http.StatusBadRequest, i18n.Err("hub-iphone.err_no_device"))
		return
	}
	p := h.iphoneOf(m)
	if p == nil {
		writeErr(w, http.StatusNotFound, i18n.Err("hub-iphone.err_off"))
		return
	}
	if action == "pair" && !p.Enabled {
		writeErr(w, http.StatusNotFound, i18n.Err("hub-iphone.err_not_enabled"))
		return
	}
	if !m.isOnline() {
		writeErr(w, http.StatusConflict, i18n.Err("hub-computer.err_offline"))
		return
	}
	if !m.hasIPhoneProgram() {
		writeErr(w, http.StatusConflict, i18n.Err("hub-iphone.note_outdated"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	asked := time.Now()
	params := map[string]any{}
	if in.Device != "" {
		params["device"] = in.Device
	}
	var got proto.IPhone
	if err := m.callBuiltin(ctx, "iphone."+action, params, &got); err != nil {
		h.writeComputerErr(w, r, http.StatusConflict, err)
		return
	}
	h.logf("[%s] iphone %s", m.cfg.ID, action) // never which iPhone
	got.Enabled = false
	for i := range got.Devices {
		got.Devices[i].Allowed = false
	}
	if h.keepIPhone(m, got, asked) {
		h.broadcastMachines()
	}
	writeJSON(w, i18n.IPhoneStatus(h.requestLang(r), h.iphoneOf(m)))
}

// hasIPhoneProgram: m's node has the built-in program iphone (node.info's builtin_programs); one from before has no
// way to an iPhone.
func (m *Machine) hasIPhoneProgram() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Contains(m.info.BuiltinPrograms, iphoneExtensionID)
}

// iphoneSeen is one iPhone as the app on an iPhone lists them to find itself: the device, the computers that reach it, and
// whether it consented.
type iphoneSeen struct {
	proto.IPhoneDevice
	Machines  []iphoneSeenMac `json:"machines"`
	Consented bool            `json:"consented"`
}

type iphoneSeenMac struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// iphoneList is GET /api/v1/iphone: whether the extension is on and every iPhone the Macs reach (each once, with the
// Macs that reach it), for Settings › Connectors › This iPhone.
type iphoneList struct {
	On      bool         `json:"on"`
	Devices []iphoneSeen `json:"devices"`
}

// iphoneDevices is every iPhone the computers reported, each once, with the computers that reach it: as the computer
// that best reaches it reports it (allowed there first, then connected).
func (h *Manager) iphoneDevices() []iphoneSeen {
	better := func(a, b proto.IPhoneDevice) bool {
		if a.Allowed != b.Allowed {
			return a.Allowed
		}
		return a.Connected && !b.Connected
	}
	s := h.iphone()
	var out []iphoneSeen
	at := map[string]int{}
	for _, m := range h.machinesNow() {
		p := h.iphoneOf(m)
		if p == nil {
			continue
		}
		for _, d := range p.Devices {
			if d.ID == "" {
				continue
			}
			mac := iphoneSeenMac{ID: m.cfg.ID, Name: m.cfg.Name, Enabled: p.Enabled}
			if i, ok := at[d.ID]; ok {
				out[i].Machines = append(out[i].Machines, mac)
				if better(d, out[i].IPhoneDevice) {
					out[i].IPhoneDevice = d
				}
				continue
			}
			at[d.ID] = len(out)
			out = append(out, iphoneSeen{IPhoneDevice: d, Machines: []iphoneSeenMac{mac}, Consented: slices.Contains(s.Phones, d.ID)})
		}
	}
	return out
}

func (h *Manager) getIPhoneList(w http.ResponseWriter, r *http.Request) {
	s := h.iphone()
	out := iphoneList{On: s.On, Devices: []iphoneSeen{}}
	if s.On {
		var wait []chan struct{}
		for _, m := range h.machinesNow() {
			if h.iphoneOf(m) == nil || !m.isOnline() {
				continue
			}
			done := make(chan struct{})
			wait = append(wait, done)
			go func() { defer close(done); h.refreshIPhone(r.Context(), m) }()
		}
		for _, c := range wait {
			<-c
		}
		l := h.requestLang(r)
		for _, d := range h.iphoneDevices() {
			d.Note = i18n.ComputerText(l, d.Note)
			out.Devices = append(out.Devices, d)
		}
	}
	writeJSON(w, out)
}

// putIPhoneConsent: PUT /api/v1/iphone/consent {"device": id, "allowed": bool}, an iPhone's own "Allow the assistant
// to operate this iPhone". Only from the user's own signed-in app (never a master token or an extension): the
// consent is that iPhone's.
func (h *Manager) putIPhoneConsent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Device  string `json:"device"`
		Allowed bool   `json:"allowed"`
	}
	if err := decodeJSONBody(w, r, &in, 4<<10); err != nil {
		h.writeComputerBodyErr(w, r, err)
		return
	}
	in.Device = strings.TrimSpace(in.Device)
	if !byOwnDevice(r.Context()) || principalOf(r.Context()).Extension != "" {
		writeErr(w, http.StatusForbidden, i18n.Err("hub-iphone.err_consent_device"))
		return
	}
	if !h.iphone().On && in.Allowed { // taking it back always works: the consent must not wait for the extension
		writeErr(w, http.StatusNotFound, i18n.Err("hub-iphone.err_off"))
		return
	}
	if in.Device == "" || len(in.Device) > 200 {
		writeErr(w, http.StatusBadRequest, i18n.Err("hub-iphone.err_no_device"))
		return
	}
	if err := h.setExtensionList(r.Context(), iphoneExtensionID, "phones", in.Device, in.Allowed); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	h.logf("iphone consent %v from device %s", in.Allowed, principalOf(r.Context()).ID)
	for _, d := range h.iphoneDevices() {
		if d.ID == in.Device {
			d.Note = i18n.ComputerText(h.requestLang(r), d.Note)
			writeJSON(w, d)
			return
		}
	}
	writeJSON(w, iphoneSeen{IPhoneDevice: proto.IPhoneDevice{ID: in.Device}, Machines: []iphoneSeenMac{}, Consented: in.Allowed})
}

// ---- the assistant's tools ----
// Tool diagnostics below are model instructions and raw node output, not client interface copy.

// iphoneTools are the assistant's tools while some iPhone is allowed: the extension on, a Mac on, that iPhone consented.
func (h *Manager) iphoneTools() []proto.Tool {
	if s := h.iphone(); !s.On || len(s.Machines) == 0 || len(s.Phones) == 0 {
		return nil
	}
	for _, d := range h.iphoneDevices() {
		if d.Allowed {
			return []proto.Tool{mcpTool("iphone_status"), mcpTool("iphone_look"), mcpTool("iphone_do")}
		}
	}
	return nil
}

func isIPhoneTool(name string) bool {
	return name == "iphone_status" || name == "iphone_look" || name == "iphone_do"
}

// iphoneApprovals: the most approvals one iphone_do may ask for (a step after another that needs one).
const iphoneApprovals = 8

// callIPhoneTool runs iphone_status, iphone_look or iphone_do for the assistant's conversation sessionID.
func (h *Manager) callIPhoneTool(ctx context.Context, sessionID, name string, args json.RawMessage) proto.ToolResult {
	var a struct {
		Phone      string          `json:"phone"`
		Screenshot *bool           `json:"screenshot"`
		Query      string          `json:"query"`
		Steps      json.RawMessage `json:"steps"`
		Observe    json.RawMessage `json:"observe"`
		TimeoutMS  int             `json:"timeout_ms"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			return proto.TextResult("bad arguments: "+err.Error(), true)
		}
	}
	session := "persona-" + convID(sessionID)
	if name == "iphone_status" {
		return h.iphoneStatus(ctx, session)
	}
	d, m, err := h.iphoneTarget(ctx, a.Phone)
	if err != nil {
		return proto.TextResult(err.Error(), true)
	}
	in := map[string]any{}
	op := "look"
	if name == "iphone_look" {
		in["screenshot"] = a.Screenshot == nil || *a.Screenshot
		if a.Query != "" {
			in["query"] = a.Query
		}
	} else {
		var steps []map[string]any
		if json.Unmarshal(a.Steps, &steps) != nil || len(steps) == 0 {
			return proto.TextResult(`give steps: [{"action": "tap", "target": 3}, {"action": "type", "text": "…"}], in order`, true)
		}
		op, in["steps"] = "do", a.Steps
		if len(a.Observe) > 0 {
			in["observe"] = a.Observe
		}
	}
	raw, _ := json.Marshal(in)
	by := h.assistantAsker(sessionID, m.cfg.ID)
	what := iphoneLogWhat(op, a.Steps)
	declined := func() proto.ToolResult {
		h.logf("[%s] %s declined on the phone (%s)", m.cfg.ID, name, what)
		return proto.TextResult("The user declined this on the phone. Do not retry it unless they ask.", true)
	}
	if op == "do" && h.iphone().Confirm && !h.approve(ctx, by, name, iphoneSummary(h.userLang(), d.Name, a.Steps), args) {
		return declined()
	}
	call := proto.IPhoneCall{Device: d.ID, Session: session, Op: op, Args: raw, TimeoutMS: a.TimeoutMS}
	start := time.Now()
	res, err := h.iphoneCall(ctx, m, call)
	for n := 0; err == nil && res.NeedsApproval; n++ {
		if n == iphoneApprovals || res.ApprovalID == "" {
			err = errors.New("the iPhone asked for approval again and again: stop and tell the user what is left to do")
			break
		}
		summary := i18n.T(h.userLang(), "hub-iphone.confirm_sensitive", i18n.Args{"phone": d.Name, "summary": res.ApprovalSummary})
		if !h.approve(ctx, by, name, summary, args) {
			return declined()
		}
		call.Approved = res.ApprovalID
		res, err = h.iphoneCall(ctx, m, call)
	}
	h.logf("[%s] %s for %s: %s, %v, %s", m.cfg.ID, name, convID(sessionID), what, time.Since(start).Round(time.Millisecond),
		map[bool]string{true: "failed (sent " + iphoneSentOf(err) + ")", false: "ok"}[err != nil])
	if err != nil {
		return iphoneFailure(err)
	}
	return computerResult(&proto.ComputerResult{Output: res.Output, Text: res.Text, Image: res.Image})
}

// iphoneStatus is iphone_status: each allowed iPhone as its Macs see it now, with what its node says of it (op status).
func (h *Manager) iphoneStatus(ctx context.Context, session string) proto.ToolResult {
	h.refreshIPhoneMacs(ctx)
	type phone struct {
		ID          string          `json:"id"`
		Name        string          `json:"name"`
		Model       string          `json:"model,omitempty"`
		IOS         string          `json:"ios,omitempty"`
		Mac         string          `json:"mac"`
		Connected   bool            `json:"connected"`
		Transport   string          `json:"transport,omitempty"`
		Locked      bool            `json:"locked"`
		Ready       bool            `json:"ready"`
		Note        string          `json:"note,omitempty"`
		Detail      json.RawMessage `json:"detail,omitempty"`
		DetailError string          `json:"detail_error,omitempty"`
	}
	out := struct {
		Phones     []phone `json:"phones"`
		NotAllowed int     `json:"not_allowed,omitempty"`
	}{Phones: []phone{}}
	for _, d := range h.iphoneDevices() {
		if !d.Allowed {
			out.NotAllowed++
			continue
		}
		m := h.iphoneMac(d)
		p := phone{ID: d.ID, Name: d.Name, Model: d.ProductType, IOS: d.IOS, Connected: d.Connected, Transport: d.Transport,
			Locked: d.Locked, Ready: d.Ready, Note: d.Note}
		if m != nil {
			p.Mac = m.cfg.ID
			if d.Connected {
				res, err := h.iphoneCall(ctx, m, proto.IPhoneCall{Device: d.ID, Session: session, Op: "status"})
				switch {
				case err != nil:
					p.DetailError = err.Error()
				case len(res.Output) > 0:
					p.Detail = res.Output
				case res.Text != "":
					p.Detail, _ = json.Marshal(res.Text)
				}
			}
		}
		out.Phones = append(out.Phones, p)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return proto.TextResult(string(b), false)
}

// refreshIPhoneMacs asks every Mac the extension is on for what it reaches now, all at once.
func (h *Manager) refreshIPhoneMacs(ctx context.Context) {
	var wait []chan struct{}
	for _, m := range h.machinesNow() {
		if p := h.iphoneOf(m); p == nil || !p.Enabled || !m.isOnline() {
			continue
		}
		done := make(chan struct{})
		wait = append(wait, done)
		go func() { defer close(done); h.refreshIPhone(ctx, m) }()
	}
	for _, c := range wait {
		<-c
	}
}

// iphoneTarget is the allowed iPhone the assistant named (by id or name; the only one when it named none) and the
// computer to reach it through: from what the computers said last, and only when that does not settle it, from what
// they say now (each look and step would otherwise wait for every computer's status first).
func (h *Manager) iphoneTarget(ctx context.Context, name string) (iphoneSeen, *Machine, error) {
	d, m, err := h.iphonePick(name)
	if err == nil && m != nil {
		return d, m, nil
	}
	h.refreshIPhoneMacs(ctx)
	d, m, err = h.iphonePick(name)
	if err != nil {
		return iphoneSeen{}, nil, err
	}
	if m == nil {
		return iphoneSeen{}, nil, fmt.Errorf("no computer that reaches %s is online", d.Name)
	}
	return d, m, nil
}

// iphonePick is iphoneTarget on what the computers said last: the iPhone (an error when none or several match) and
// the computer to reach it through (nil when none is online).
func (h *Manager) iphonePick(name string) (iphoneSeen, *Machine, error) {
	var allowed []iphoneSeen
	for _, d := range h.iphoneDevices() {
		if d.Allowed {
			allowed = append(allowed, d)
		}
	}
	names := make([]string, len(allowed))
	for i, d := range allowed {
		names[i] = fmt.Sprintf("%s (%s)", d.Name, d.ID)
	}
	list := strings.Join(names, ", ")
	if list == "" {
		list = "none"
	}
	name = strings.TrimSpace(name)
	var match []iphoneSeen
	for _, d := range allowed {
		if name == "" || d.ID == name {
			match = append(match, d)
		}
	}
	if name != "" && len(match) == 0 {
		for _, d := range allowed {
			if strings.EqualFold(d.Name, name) {
				match = append(match, d)
			}
		}
	}
	switch {
	case len(match) == 1:
		return match[0], h.iphoneMac(match[0]), nil
	case len(match) > 1:
		return iphoneSeen{}, nil, fmt.Errorf("more than one iPhone matches: give phone as an id (one of: %s)", list)
	default:
		return iphoneSeen{}, nil, fmt.Errorf("no iPhone %q the user allowed (allowed: %s); the user allows one in that iPhone's Persona app and turns its computer on on that computer's page", name, list)
	}
}

// iphoneMac is the online Mac, turned on, to reach d through: one where it is connected first.
func (h *Manager) iphoneMac(d iphoneSeen) *Machine {
	var found *Machine
	for _, mac := range d.Machines {
		m := h.machineByID(mac.ID)
		if m == nil || !mac.Enabled || !m.isOnline() {
			continue
		}
		p := h.iphoneOf(m)
		if p == nil {
			continue
		}
		for _, dev := range p.Devices {
			if dev.ID == d.ID && dev.Allowed && dev.Connected {
				return m
			}
		}
		if found == nil {
			found = m
		}
	}
	return found
}

// The node's time for an iphone.call (TimeoutMS), and how much longer the manager waits for its answer.
const (
	iphoneTimeoutDefault = 30 * time.Second
	iphoneTimeoutMost    = 120 * time.Second
	iphoneTimeoutSlack   = 5 * time.Second
)

// iphoneNotSent is a call that never left the manager: the extension off, the computer not on or offline, the iPhone
// not allowed there, the node too old. Safe to try again once that changed.
func iphoneNotSent(code, format string, args ...any) error {
	return &rpc.Error{Code: code, Message: fmt.Sprintf(format, args...), Data: iphoneSentData(proto.IPhoneNotSent)}
}

func iphoneSentData(sent string) json.RawMessage {
	raw, _ := json.Marshal(proto.IPhoneErrorData{Sent: sent})
	return raw
}

// iphoneCall is one iphone.call on m's node. It checks again, here, that the extension is on, m is on, online and has
// the program, and the iPhone is allowed on m (a caller's earlier look may be stale); any of them failing is an error
// that says nothing was sent. The node gets call.TimeoutMS (1–120 s, under a second rounded up, 30 s when 0); the
// manager waits 5 s more. Every error is an *rpc.Error whose data says whether anything may have reached the iPhone:
// the node's own as it is, no for a computer that could not be reached, unknown for a call that left with no answer.
func (h *Manager) iphoneCall(ctx context.Context, m *Machine, call proto.IPhoneCall) (proto.IPhoneResult, error) {
	var res proto.IPhoneResult
	p := h.iphoneOf(m)
	switch {
	case p == nil:
		return res, iphoneNotSent("iphone_off", "the phone control extension is off, or %s cannot reach iPhones", m.cfg.ID)
	case !p.Enabled:
		return res, iphoneNotSent("not_enabled", "phone control is not turned on for %s", m.cfg.ID)
	case !m.isOnline():
		return res, iphoneNotSent("offline", "%s is offline", m.cfg.ID)
	case !m.hasIPhoneProgram():
		return res, iphoneNotSent("outdated", "the personad on %s is too old to operate iPhones: the user redeploys it", m.cfg.ID)
	}
	allowed := false
	for _, d := range p.Devices {
		allowed = allowed || (d.ID == call.Device && d.Allowed)
	}
	if !allowed {
		return res, iphoneNotSent("not_allowed", "the iPhone %s is not allowed on %s: the user allows it in that iPhone's Persona app", call.Device, m.cfg.ID)
	}
	wait := time.Duration(call.TimeoutMS) * time.Millisecond
	switch {
	case call.TimeoutMS <= 0:
		wait, call.TimeoutMS = iphoneTimeoutDefault, 0
	case wait < time.Second:
		wait, call.TimeoutMS = time.Second, 1000
	case wait > iphoneTimeoutMost:
		wait, call.TimeoutMS = iphoneTimeoutMost, int(iphoneTimeoutMost/time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(ctx, wait+iphoneTimeoutSlack)
	defer cancel()
	if err := m.callBuiltin(ctx, "iphone.call", call, &res); err != nil {
		var coded *rpc.Error
		switch {
		case errors.As(err, &coded) && len(coded.Data) > 0:
			return res, coded
		case errors.Is(err, errOffline):
			return res, iphoneNotSent("offline", "%s", err.Error())
		}
		// The call left for the node and no answer came back: it may have been done.
		return res, &rpc.Error{Code: "unknown", Message: err.Error(), Data: iphoneSentData(proto.IPhoneMaybeSent)}
	}
	return res, nil
}

// iphoneSentOf is what a failed call says was sent: no, or unknown (also when it says neither).
func iphoneSentOf(err error) string {
	var e *rpc.Error
	var d proto.IPhoneErrorData
	if errors.As(err, &e) && json.Unmarshal(e.Data, &d) == nil && d.Sent == proto.IPhoneNotSent {
		return proto.IPhoneNotSent
	}
	return proto.IPhoneMaybeSent
}

// iphoneFailure is a failed call for the model: what went wrong, and whether to try again or look first.
func iphoneFailure(err error) proto.ToolResult {
	if iphoneSentOf(err) == proto.IPhoneNotSent {
		return proto.TextResult(err.Error()+" (nothing was sent to the iPhone)", true)
	}
	return proto.TextResult(err.Error()+" (some of it may have happened on the iPhone: look at its screen with iphone_look before anything else, and do not repeat the steps blindly)", true)
}

// iphoneSummary is the card of "ask before every action", in l: on which iPhone, the steps' actions.
func iphoneSummary(l i18n.Lang, phone string, steps json.RawMessage) string {
	return i18n.T(l, "hub-iphone.confirm_steps", i18n.Args{"phone": phone,
		"steps": strings.Join(iphoneActions(steps), i18n.T(l, "hub-computer.list_sep"))})
}

// iphoneLogWhat is the log's account of a call: look, or the steps' actions, never what is typed or tapped.
func iphoneLogWhat(op string, steps json.RawMessage) string {
	if op != "do" {
		return op
	}
	return "do [" + strings.Join(iphoneActions(steps), " ") + "]"
}

// iphoneActions are the actions of iphone_do's steps ({"action": "tap", …}); one without is "step".
func iphoneActions(steps json.RawMessage) []string {
	var list []struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(steps, &list)
	out := make([]string, 0, len(list))
	for _, s := range list {
		a := strings.TrimSpace(s.Action)
		if a == "" || len(a) > 32 {
			a = "step"
		}
		out = append(out, a)
	}
	return out
}
