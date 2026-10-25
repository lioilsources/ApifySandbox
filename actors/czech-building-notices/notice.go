package main

import (
	"crypto/sha1"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/lioilsources/apifysandbox/ruian"
	"github.com/lioilsources/czech-building-notices/internal/classify"
	"github.com/lioilsources/czech-building-notices/internal/desky"
	"github.com/lioilsources/czech-building-notices/internal/offices"
)

// OfficeOut is the publishing authority as shown in output.
type OfficeOut struct {
	Name  string `json:"name"`
	ICO   string `json:"ico"`
	Obec  string `json:"obec,omitempty"`
	ORP   string `json:"orp,omitempty"`
	Okres string `json:"okres,omitempty"`
	Kraj  string `json:"kraj,omitempty"`
}

// ParcelOut is a parcel named in the notice; RÚIAN fields are filled once
// the reference is resolved.
type ParcelOut struct {
	Number     string        `json:"number"`
	KU         *ruian.Unit   `json:"ku,omitempty"`
	Raw        string        `json:"raw"`
	Confidence float64       `json:"confidence"`
	Resolved   bool          `json:"resolved"`
	ID         int64         `json:"id,omitempty"`
	AreaM2     float64       `json:"areaM2,omitempty"`
	LandType   string        `json:"landType,omitempty"`
	Centroid   *ruian.LatLon `json:"centroid,omitempty"`
	VDP        string        `json:"vdp,omitempty"`
	Note       string        `json:"note,omitempty"`
}

// Notice is one dataset item / MCP result.
type Notice struct {
	ID              string           `json:"id"`
	Type            classify.Type    `json:"type"`
	TypeLabel       string           `json:"typeLabel"`
	Title           string           `json:"title"`
	Description     string           `json:"description,omitempty"`
	Posted          string           `json:"posted,omitempty"`
	RelevantUntil   string           `json:"relevantUntil,omitempty"`
	Expired         bool             `json:"expired"`
	CaseNumber      string           `json:"caseNumber,omitempty"`
	FileMark        string           `json:"fileMark,omitempty"`
	Agenda          []string         `json:"agenda,omitempty"`
	Office          OfficeOut        `json:"office"`
	Territory       *ruian.Territory `json:"territory,omitempty"`
	TerritorySource string           `json:"territorySource"` // parcel | ku | office
	KUs             []ruian.Unit     `json:"kus"`
	Parcels         []ParcelOut      `json:"parcels"`
	ProtectedAreas  []ruian.Area     `json:"protectedAreas,omitempty"`
	MiningAreas     []ruian.Area     `json:"miningAreas,omitempty"`
	Documents       []desky.Doc      `json:"documents,omitempty"`
	URL             string           `json:"url,omitempty"`
	Board           BoardOut         `json:"board"`
	FirstSeenAt     string           `json:"firstSeenAt"`

	territories []ruian.Territory // every k.ú. territory, for region filters
	refs        []ruian.ParcelRef
	posted      time.Time
	firstSeen   time.Time
}

type BoardOut struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Dataset string `json:"dataset"`
}

func noticeID(b desky.Board, in desky.Info) string {
	key := in.IRI
	if key == "" {
		key = b.URL + "|" + in.URL + "|" + in.Title + "|" + in.Posted.Format(time.DateOnly)
	}
	h := sha1.Sum([]byte(key))
	return hex.EncodeToString(h[:8])
}

// dedupeKey spots the same notice on two boards (kraj and obec post the
// same veřejná vyhláška): same case number and same title.
func dedupeKey(n *Notice) string {
	if n.CaseNumber == "" {
		return n.ID
	}
	return ruian.Fold(strings.Join(strings.Fields(n.CaseNumber), "")) + "|" + ruian.Fold(n.Title)
}

const maxDescription = 600

// boardCtx is computed once per board: the office's territory, the k.ú.
// its notices most likely concern, and a parcel hint.
type boardCtx struct {
	off     offices.Office
	officeT *ruian.Territory
	cands   []ruian.Unit // k.ú. of the municipality (or the whole ORP for an ORP seat)
	hint    string       // k.ú. code when the municipality has exactly one k.ú.
}

var municipalPrefix = []string{"obec ", "mesto ", "statutarni mesto ", "mestys ", "mestska cast ", "hlavni mesto ", "urad mestske", "urad mestskeho obvodu", "mestsky obvod "}

func newBoardCtx(g *ruian.Gazetteer, off offices.Office) *boardCtx {
	bc := &boardCtx{off: off}
	if g == nil || off.ObecCode == 0 {
		return bc
	}
	t, ok := g.ObecTerritory(off.ObecCode)
	if !ok {
		return bc
	}
	bc.officeT = &t
	name := ruian.Fold(off.Name)
	municipal := false
	for _, p := range municipalPrefix {
		if strings.HasPrefix(name, p) {
			municipal = true
			break
		}
	}
	if !municipal { // regions, ministries, agencies: notices concern anywhere in their area
		return bc
	}
	own := g.KUsOfObec(off.ObecCode)
	if len(own) == 1 {
		bc.hint = strconv.Itoa(own[0].Code)
	}
	if t.ORP.Name != "" && ruian.Fold(t.ORP.Name) == ruian.Fold(t.Obec.Name) {
		bc.cands = g.KUsOfORP(t.ORP.Code) // ORP seat: its building office serves the whole district
	} else {
		bc.cands = own
	}
	return bc
}

// buildNotice turns a board item into a Notice, or nil if it is not about
// building or land. g may be nil (no k.ú. resolution).
func buildNotice(b desky.Board, in desky.Info, bc *boardCtx, g *ruian.Gazetteer, now time.Time) *Notice {
	off := bc.off
	typ := classify.Classify(in.Title, in.Description, strings.Join(append(append([]string{}, in.Agenda...), in.Department), " "))
	if typ == "" {
		return nil
	}
	n := &Notice{
		ID: noticeID(b, in), Type: typ, TypeLabel: classify.Labels[typ],
		Title:      classify.Redact(strings.Join(strings.Fields(in.Title), " ")),
		CaseNumber: in.CaseNumber, FileMark: in.FileMark, Agenda: in.Agenda,
		URL:   in.URL,
		Board: BoardOut{Title: b.Title, URL: b.URL, Dataset: b.Dataset},
		KUs:   []ruian.Unit{}, Parcels: []ParcelOut{}, posted: in.Posted,
	}
	if !in.Posted.IsZero() {
		n.Posted = in.Posted.Format(time.RFC3339)
	}
	if !in.RelevantUntil.IsZero() {
		n.RelevantUntil = in.RelevantUntil.Format(time.RFC3339)
		n.Expired = in.RelevantUntil.Before(now)
	}
	// After relevantní_do only metadata and the link stay (data minimisation).
	if !n.Expired {
		d := classify.Redact(strings.Join(strings.Fields(in.Description), " "))
		if r := []rune(d); len(r) > maxDescription {
			cut := string(r[:maxDescription])
			if i := strings.LastIndex(cut, " "); i > maxDescription/2 {
				cut = cut[:i]
			}
			d = cut + " …"
		}
		n.Description = d
		// File names often carry the applicant's surname ("…_Nováková.pdf");
		// keep only the links.
		for _, doc := range in.Documents {
			n.Documents = append(n.Documents, desky.Doc{URL: doc.URL})
		}
	}
	n.Office = OfficeOut{Name: firstNonEmpty(off.Name, b.Title), ICO: firstNonEmpty(off.ICO, b.PublisherICO)}
	officeT := bc.officeT
	if officeT != nil {
		t := officeT
		n.Office.Obec, n.Office.ORP, n.Office.Okres, n.Office.Kraj = t.Obec.Name, t.ORP.Name, t.Okres.Name, t.Kraj.Name
	}

	text := in.Title + "\n" + in.Description
	if g != nil {
		n.KUs = ruian.ExtractKUs(text, g)
		hint := bc.hint
		if len(n.KUs) == 0 && len(bc.cands) > 0 {
			// "Pekařská, Staré Brno": k.ú. names of the office's area without "k. ú."
			if m := ruian.MatchKUNames(text, bc.cands); len(m) > 0 {
				n.KUs = m
				if len(m) == 1 {
					hint = strconv.Itoa(m[0].Code)
				}
			}
		}
		if n.KUs == nil {
			n.KUs = []ruian.Unit{}
		}
		n.refs = ruian.ExtractParcelRefs(text, g, hint)
		if len(n.refs) > 40 {
			n.refs = n.refs[:40]
		}
		for _, r := range n.refs {
			n.Parcels = append(n.Parcels, ParcelOut{Number: r.Number, KU: r.KU, Raw: r.Raw, Confidence: r.Confidence})
			if r.KU != nil && !hasUnit(n.KUs, r.KU.Code) {
				n.KUs = append(n.KUs, *r.KU)
			}
		}
		for _, k := range n.KUs {
			if t, ok := g.Territory(k.Code); ok {
				n.territories = append(n.territories, t)
			}
		}
	}
	switch {
	case len(n.territories) > 0:
		t := n.territories[0]
		n.Territory, n.TerritorySource = &t, "ku"
	case officeT != nil:
		n.Territory, n.TerritorySource = officeT, "office"
		n.territories = []ruian.Territory{*officeT}
	default:
		n.TerritorySource = "unknown"
	}
	return n
}

func hasUnit(us []ruian.Unit, code int) bool {
	for _, u := range us {
		if u.Code == code {
			return true
		}
	}
	return false
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
