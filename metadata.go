package omt

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

const (
	SubscribeVideoXML = `<OMTSubscribe Video="true" />`
	SubscribeAudioXML = `<OMTSubscribe Audio="true" />`
	SubscribeMetaXML  = `<OMTSubscribe Metadata="true" />`
	PreviewOnXML      = `<OMTSettings Preview="true" />`
	PreviewOffXML     = `<OMTSettings Preview="false" />`
	// The double equals in Program== is intentional: libomtnet matches these
	// four strings verbatim and does not XML-parse them.
	TallyPreviewXML        = `<OMTTally Preview="true" Program=="false" />`
	TallyProgramXML        = `<OMTTally Preview="false" Program=="true" />`
	TallyPreviewProgramXML = `<OMTTally Preview="true" Program=="true" />`
	TallyNoneXML           = `<OMTTally Preview="false" Program=="false" />`
)

// QualityXML returns the suggested quality document.
func QualityXML(q Quality) string {
	name := "Default"
	switch q {
	case QualityLow:
		name = "Low"
	case QualityMedium:
		name = "Medium"
	case QualityHigh:
		name = "High"
	}
	return fmt.Sprintf(`<OMTSettings Quality="%s" />`, name)
}

func qualityFromName(s string) (Quality, bool) {
	switch s {
	case "Default":
		return QualityDefault, true
	case "Low":
		return QualityLow, true
	case "Medium":
		return QualityMedium, true
	case "High":
		return QualityHigh, true
	}
	return 0, false
}

type xmlNode struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Children []xmlNode  `xml:",any"`
	Text     string     `xml:",chardata"`
}

func parseXML(s string) (xmlNode, error) {
	var n xmlNode
	if err := xml.Unmarshal([]byte(s), &n); err != nil {
		return n, err
	}
	return n, nil
}

func (n xmlNode) attr(name string) (string, bool) {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value, true
		}
	}
	return "", false
}

func (n xmlNode) attrBool(name string) (bool, bool) {
	v, ok := n.attr(name)
	if !ok {
		return false, false
	}
	switch strings.ToLower(v) {
	case "true", "1", "yes":
		return true, true
	case "false", "0", "no":
		return false, true
	}
	return false, false
}

func (n xmlNode) child(name string) (xmlNode, bool) {
	for _, c := range n.Children {
		if c.XMLName.Local == name {
			return c, true
		}
	}
	return xmlNode{}, false
}

func (n xmlNode) childText(name string) string {
	if c, ok := n.child(name); ok {
		return strings.TrimSpace(c.Text)
	}
	return ""
}

// Subscribe describes an OMTSubscribe document.
type Subscribe struct{ Video, Audio, Metadata bool }

func ParseSubscribe(s string) (Subscribe, bool) {
	n, err := parseXML(s)
	if err != nil || n.XMLName.Local != "OMTSubscribe" {
		return Subscribe{}, false
	}
	v, _ := n.attrBool("Video")
	a, _ := n.attrBool("Audio")
	m, _ := n.attrBool("Metadata")
	return Subscribe{Video: v, Audio: a, Metadata: m}, true
}

// Settings describes an OMTSettings document.
type Settings struct {
	Preview *bool
	Quality *Quality
}

func ParseSettings(s string) (Settings, bool) {
	n, err := parseXML(s)
	if err != nil || n.XMLName.Local != "OMTSettings" {
		return Settings{}, false
	}
	var out Settings
	if v, ok := n.attrBool("Preview"); ok {
		out.Preview = &v
	}
	if q, ok := n.attr("Quality"); ok {
		if qq, ok := qualityFromName(q); ok {
			out.Quality = &qq
		}
	}
	return out, true
}

// ParseTally parses either a libomtnet exact tally string or well-formed XML.
func ParseTally(s string) (Tally, bool) {
	switch s {
	case TallyPreviewXML:
		return Tally{Preview: 1, Program: 0}, true
	case TallyProgramXML:
		return Tally{Preview: 0, Program: 1}, true
	case TallyPreviewProgramXML:
		return Tally{Preview: 1, Program: 1}, true
	case TallyNoneXML:
		return Tally{Preview: 0, Program: 0}, true
	}
	n, err := parseXML(s)
	if err != nil || n.XMLName.Local != "OMTTally" {
		return Tally{}, false
	}
	p, _ := n.attrBool("Preview")
	g, _ := n.attrBool("Program")
	return Tally{Preview: boolInt(p), Program: boolInt(g)}, true
}

func boolInt(v bool) int32 {
	if v {
		return 1
	}
	return 0
}

func ParseSenderInfo(s string) (SenderInfo, bool) {
	n, err := parseXML(s)
	if err != nil || n.XMLName.Local != "OMTInfo" {
		return SenderInfo{}, false
	}
	pn, _ := n.attr("ProductName")
	mf, _ := n.attr("Manufacturer")
	ver, _ := n.attr("Version")
	return SenderInfo{ProductName: pn, Manufacturer: mf, Version: ver}, true
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// XML renders the sender info document.
func (s SenderInfo) XML() string {
	return fmt.Sprintf(`<OMTInfo ProductName="%s" Manufacturer="%s" Version="%s" />`,
		xmlEscape(s.ProductName), xmlEscape(s.Manufacturer), xmlEscape(s.Version))
}

// Address is the discovery-server OMTAddress document.
type Address struct {
	Name      string
	Port      int
	Removed   bool
	Addresses []string
}

func (a Address) XML() string {
	var b strings.Builder
	b.WriteString("<OMTAddress>\n  <Name>")
	b.WriteString(xmlEscape(a.Name))
	b.WriteString("</Name>\n  <Port>")
	b.WriteString(strconv.Itoa(a.Port))
	b.WriteString("</Port>\n")
	if a.Removed {
		b.WriteString("  <Removed>True</Removed>\n")
	}
	b.WriteString("  <Addresses>\n")
	if len(a.Addresses) == 0 {
		b.WriteString("    <IPAddress>0.0.0.0</IPAddress>\n")
	} else {
		for _, ip := range a.Addresses {
			b.WriteString("    <IPAddress>")
			b.WriteString(xmlEscape(ip))
			b.WriteString("</IPAddress>\n")
		}
	}
	b.WriteString("  </Addresses>\n</OMTAddress>")
	return b.String()
}

func ParseAddress(s string) (Address, error) {
	n, err := parseXML(s)
	if err != nil {
		return Address{}, err
	}
	if n.XMLName.Local != "OMTAddress" {
		return Address{}, protocolErr("expected OMTAddress, got %s", n.XMLName.Local)
	}
	name := n.childText("Name")
	if name == "" {
		return Address{}, protocolErr("OMTAddress missing Name")
	}
	port, _ := strconv.Atoi(n.childText("Port"))
	removed := strings.EqualFold(n.childText("Removed"), "true")
	var addrs []string
	if as, ok := n.child("Addresses"); ok {
		for _, c := range as.Children {
			if c.XMLName.Local == "IPAddress" || c.XMLName.Local == "Address" {
				t := strings.TrimSpace(c.Text)
				if t != "" {
					addrs = append(addrs, t)
				}
			}
		}
	}
	return Address{Name: name, Port: port, Removed: removed, Addresses: addrs}, nil
}

func metadataXML(data []byte) string {
	end := len(data)
	for i, b := range data {
		if b == 0 {
			end = i
			break
		}
	}
	return string(data[:end])
}
