// Package browser defines browser contracts and the Electron client bridge.
package browser

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	ErrUnavailable = errors.New("browser unavailable")
	ErrTabNotFound = errors.New("browser tab not found")
	ErrTabRequired = errors.New("browser tab id required")
	ErrTabLimit    = errors.New("browser tab limit reached")
)

type Service interface {
	ProcessMode(ctx context.Context, sessionID string) string
	CreateTab(ctx context.Context, sessionID string) (TabSnapshot, error)
	OpenNewTab(ctx context.Context, sessionID, rawURL string) (TabSnapshot, error)
	ListTabs(ctx context.Context, sessionID string) ([]TabSnapshot, error)
	GetTab(ctx context.Context, sessionID, tabID string) (TabSnapshot, error)
	Recover(ctx context.Context, sessionID string, hint RecoverHint) (TabSnapshot, error)
	CloseSessionBrowser(ctx context.Context, sessionID string) error
	ReleaseTab(ctx context.Context, sessionID, tabID string) error
	CloseWindow(ctx context.Context, sessionID, tabID, windowID string) error
	ReleaseSession(ctx context.Context, sessionID string) error
	Open(ctx context.Context, sessionID, tabID, rawURL string) (TabSnapshot, error)
	Back(ctx context.Context, sessionID, tabID string) (TabSnapshot, error)
	Forward(ctx context.Context, sessionID, tabID string) (TabSnapshot, error)
	Reload(ctx context.Context, sessionID, tabID string) (TabSnapshot, error)
	Observe(ctx context.Context, sessionID, tabID string, opts ObserveOptions) (ObserveResult, error)
	Screenshot(ctx context.Context, sessionID, tabID string, opts ScreenshotOptions) (ScreenshotResult, error)
	Click(ctx context.Context, sessionID, tabID string, in ClickInput) (ActionResult, error)
	Select(ctx context.Context, sessionID, tabID string, in SelectInput) (ActionResult, error)
	Type(ctx context.Context, sessionID, tabID string, in TypeInput) (ActionResult, error)
	Scroll(ctx context.Context, sessionID, tabID string, in ScrollInput) (ActionResult, error)
	Dialog(ctx context.Context, sessionID, tabID string, in DialogInput) (ActionResult, error)
	Hover(ctx context.Context, sessionID, tabID string, in HoverInput) (ActionResult, error)
	Drag(ctx context.Context, sessionID, tabID string, in DragInput) (ActionResult, error)
	PressKey(ctx context.Context, sessionID, tabID string, in KeyInput) (ActionResult, error)
	Wait(ctx context.Context, sessionID, tabID string, in WaitInput) (ActionResult, error)
	Close() error
}

type MetadataRecoverySupport interface {
	SupportsMetadataRecovery() bool
}

// FileAccessRevoker removes cached project-directory grants from live browser
// tabs. Tabs that are still displaying file:// content are closed.
type FileAccessRevoker interface {
	RevokeFileAccess(ctx context.Context, sessionID string) ([]string, error)
}

type TabSnapshot struct {
	ID           string    `json:"id"`
	SessionID    string    `json:"sessionID"`
	TargetID     string    `json:"targetID,omitempty"`
	URL          string    `json:"url"`
	Title        string    `json:"title"`
	FaviconURL   string    `json:"faviconURL,omitempty"`
	Mode         string    `json:"mode,omitempty"`
	CanGoBack    bool      `json:"canGoBack,omitempty"`
	CanGoForward bool      `json:"canGoForward,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type RecoverHint struct {
	TabID      string
	URL        string
	Title      string
	FaviconURL string
	Mode       string
	CreatedAt  time.Time
	FileRoot   string
}

type ObserveOptions struct {
	FrameID      string `json:"frameID,omitempty"`
	WindowID     string `json:"windowID,omitempty"`
	MaxTextChars int
	MaxElements  int
	Offset       int
}

type FrameInfo struct {
	FrameID       string `json:"frameID"`
	ParentFrameID string `json:"parentFrameID"`
	URL           string `json:"url"`
	Name          string `json:"name"`
	Main          bool   `json:"main"`
}

type WindowInfo struct {
	WindowID       string `json:"windowID"`
	ParentWindowID string `json:"parentWindowID"`
	URL            string `json:"url"`
	Title          string `json:"title"`
	Focused        bool   `json:"focused"`
}

type ScreenshotOptions struct {
	WindowID string `json:"windowID,omitempty"`
}

type ObserveResult struct {
	FrameID           string            `json:"frameID,omitempty"`
	WindowID          string            `json:"windowID,omitempty"`
	Frames            []FrameInfo       `json:"frames"`
	Windows           []WindowInfo      `json:"windows"`
	ObservationID     string            `json:"observationID"`
	CoordinateSpace   string            `json:"coordinateSpace"`
	Tab               TabSnapshot       `json:"tab"`
	Title             string            `json:"title"`
	URL               string            `json:"url"`
	ReadyState        string            `json:"readyState"`
	Viewport          Viewport          `json:"viewport"`
	Text              string            `json:"text"`
	TextChars         int               `json:"textChars"`
	Truncated         bool              `json:"truncated"`
	Elements          []ObservedElement `json:"elements"`
	TotalElements     int               `json:"totalElements"`
	NextOffset        *int              `json:"nextOffset"`
	UnavailableFrames []string          `json:"unavailableFrames,omitempty"`
	ScrollContainers  []ScrollContainer `json:"scrollContainers"`
}

// Bounds are CSS pixels relative to the current viewport.
type Bounds struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type Viewport struct {
	Width            float64 `json:"width"`
	Height           float64 `json:"height"`
	DevicePixelRatio float64 `json:"devicePixelRatio"`
	ScrollX          float64 `json:"scrollX"`
	ScrollY          float64 `json:"scrollY"`
}

type ScrollContainer struct {
	Ref        string  `json:"ref,omitempty"`
	Selector   string  `json:"selector"`
	Bounds     Bounds  `json:"bounds"`
	ScrollX    float64 `json:"scrollX"`
	ScrollY    float64 `json:"scrollY"`
	MaxScrollX float64 `json:"maxScrollX"`
	MaxScrollY float64 `json:"maxScrollY"`
}

type SelectOption struct {
	Label    string `json:"label"`
	Value    string `json:"value"`
	Selected bool   `json:"selected"`
	Disabled bool   `json:"disabled"`
}

type ObservedElement struct {
	Multiple       *bool          `json:"multiple,omitempty"`
	Hidden         bool           `json:"hidden,omitempty"`
	Accept         string         `json:"accept,omitempty"`
	Directory      bool           `json:"directory,omitempty"`
	Files          []UploadedFile `json:"files,omitempty"`
	AccessibleName string         `json:"accessibleName,omitempty"`
	Placeholder    string         `json:"placeholder,omitempty"`
	Checked        *bool          `json:"checked,omitempty"`
	Indeterminate  bool           `json:"indeterminate,omitempty"`
	Expanded       *bool          `json:"expanded,omitempty"`
	ReadOnly       *bool          `json:"readOnly,omitempty"`
	ValueTruncated bool           `json:"valueTruncated,omitempty"`
	Selected       *bool          `json:"selected,omitempty"`
	Value          *string        `json:"value,omitempty"`
	Options        []SelectOption `json:"options,omitempty"`
	OptionCount    *int           `json:"optionCount,omitempty"`
	Ref            string         `json:"ref,omitempty"`
	Index          int            `json:"index"`
	Tag            string         `json:"tag"`
	Text           string         `json:"text,omitempty"`
	Href           string         `json:"href,omitempty"`
	Role           string         `json:"role,omitempty"`
	AriaLabel      string         `json:"ariaLabel,omitempty"`
	Selector       string         `json:"selector,omitempty"`
	InputType      string         `json:"inputType,omitempty"`
	Disabled       bool           `json:"disabled,omitempty"`
	Bounds         Bounds         `json:"bounds"`
}

type UploadedFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

type ScreenshotResult struct {
	WindowID          string      `json:"windowID,omitempty"`
	ObservationID     string      `json:"observationID"`
	Tab               TabSnapshot `json:"tab"`
	MIME              string      `json:"mime"`
	DataBase64        string      `json:"dataBase64"`
	Size              int64       `json:"size"`
	Width             int         `json:"width,omitempty"`
	Height            int         `json:"height,omitempty"`
	ViewportWidth     int         `json:"viewportWidth,omitempty"`
	ViewportHeight    int         `json:"viewportHeight,omitempty"`
	DeviceScaleFactor float64     `json:"deviceScaleFactor,omitempty"`
	CaptureBounds     Bounds      `json:"captureBounds"`
	CapturedAt        time.Time   `json:"capturedAt"`
}

type ClickInput struct {
	FrameID       string   `json:"frameID,omitempty"`
	WindowID      string   `json:"windowID,omitempty"`
	ObservationID string   `json:"observationID,omitempty"`
	TabID         string   `json:"tabID,omitempty"`
	Selector      string   `json:"selector,omitempty"`
	X             *float64 `json:"x,omitempty"`
	Y             *float64 `json:"y,omitempty"`
	Method        string   `json:"method,omitempty"`
	Button        string   `json:"button,omitempty"`
	ClickCount    *int     `json:"clickCount,omitempty"`
	Modifiers     []string `json:"modifiers,omitempty"`
}

type TypeInput struct {
	FrameID  string `json:"frameID,omitempty"`
	WindowID string `json:"windowID,omitempty"`
	Commit   bool   `json:"commit,omitempty"`
	TabID    string `json:"tabID,omitempty"`
	Selector string `json:"selector,omitempty"`
	Text     string `json:"text"`
	Clear    bool   `json:"clear,omitempty"`
}

type SelectInput struct {
	FrameID  string    `json:"frameID,omitempty"`
	WindowID string    `json:"windowID,omitempty"`
	TabID    string    `json:"tabID,omitempty"`
	Selector string    `json:"selector"`
	Label    *string   `json:"label,omitempty"`
	Value    *string   `json:"value,omitempty"`
	Labels   *[]string `json:"labels,omitempty"`
	Values   *[]string `json:"values,omitempty"`
}

func (in SelectInput) Validate() error {
	count := 0
	for _, present := range []bool{in.Label != nil, in.Value != nil, in.Labels != nil, in.Values != nil} {
		if present {
			count++
		}
	}
	if strings.TrimSpace(in.Selector) == "" || count != 1 {
		return errors.New("selector and exactly one of label, value, labels or values are required")
	}
	for _, items := range []*[]string{in.Labels, in.Values} {
		if items == nil {
			continue
		}
		if len(*items) > 100 {
			return errors.New("invalid select options: maximum 100")
		}
		seen := make(map[string]bool)
		for _, item := range *items {
			if seen[item] {
				return errors.New("duplicate select option")
			}
			seen[item] = true
		}
	}
	return nil
}

type DragPoint struct {
	Selector      string   `json:"selector,omitempty"`
	X             *float64 `json:"x,omitempty"`
	Y             *float64 `json:"y,omitempty"`
	ObservationID string   `json:"observationID,omitempty"`
}
type DragInput struct {
	FrameID    string    `json:"frameID,omitempty"`
	WindowID   string    `json:"windowID,omitempty"`
	TabID      string    `json:"tabID,omitempty"`
	Source     DragPoint `json:"source"`
	Target     DragPoint `json:"target"`
	DurationMS *int      `json:"durationMs,omitempty"`
	Modifiers  []string  `json:"modifiers,omitempty"`
}

type ScrollInput struct {
	FrameID  string  `json:"frameID,omitempty"`
	WindowID string  `json:"windowID,omitempty"`
	TabID    string  `json:"tabID,omitempty"`
	Selector string  `json:"selector,omitempty"`
	DeltaX   float64 `json:"deltaX,omitempty"`
	DeltaY   float64 `json:"deltaY,omitempty"`
}

type ActionResult struct {
	Tab    TabSnapshot    `json:"tab"`
	Action string         `json:"action"`
	Result map[string]any `json:"result"`
}

type KeyInput struct {
	FrameID   string   `json:"frameID,omitempty"`
	WindowID  string   `json:"windowID,omitempty"`
	TabID     string   `json:"tabID,omitempty"`
	Selector  string   `json:"selector,omitempty"`
	Key       string   `json:"key"`
	Modifiers []string `json:"modifiers,omitempty"`
}

type DialogInput struct {
	WindowID   string  `json:"windowID,omitempty"`
	TabID      string  `json:"tabID,omitempty"`
	Action     string  `json:"action"`
	DialogID   string  `json:"dialogID,omitempty"`
	Accept     *bool   `json:"accept,omitempty"`
	PromptText *string `json:"promptText,omitempty"`
}

type HoverInput struct {
	FrameID       string   `json:"frameID,omitempty"`
	WindowID      string   `json:"windowID,omitempty"`
	TabID         string   `json:"tabID,omitempty"`
	Selector      string   `json:"selector,omitempty"`
	ObservationID string   `json:"observationID,omitempty"`
	X             *float64 `json:"x,omitempty"`
	Y             *float64 `json:"y,omitempty"`
}

type WaitInput struct {
	Value     *string   `json:"value,omitempty"`
	KnownIDs  *[]string `json:"knownIDs,omitempty"`
	FrameID   string    `json:"frameID,omitempty"`
	WindowID  string    `json:"windowID,omitempty"`
	TabID     string    `json:"tabID,omitempty"`
	Condition string    `json:"condition"`
	Selector  string    `json:"selector,omitempty"`
	Text      string    `json:"text,omitempty"`
	URL       string    `json:"url,omitempty"`
	TimeoutMS *int      `json:"timeoutMs,omitempty"`
}

// FileTransferService is the native browser file capability. Upload paths are
// private snapshots resolved by Core; they are not public tool arguments.
type FileTransferService interface {
	Upload(context.Context, string, string, UploadInput) (ActionResult, error)
	Downloads(context.Context, string, DownloadInput) (DownloadResult, error)
}

type UploadInput struct {
	TabID    string   `json:"tabID"`
	WindowID string   `json:"windowID,omitempty"`
	FrameID  string   `json:"frameID,omitempty"`
	Selector string   `json:"selector"`
	Files    []string `json:"files"`
}

type DownloadInput struct {
	Action     string    `json:"action"`
	TabID      string    `json:"tabID,omitempty"`
	WindowID   *string   `json:"windowID,omitempty"`
	DownloadID string    `json:"downloadID,omitempty"`
	KnownIDs   *[]string `json:"knownIDs,omitempty"`
	TimeoutMS  *int      `json:"timeoutMs,omitempty"`
}

type Download struct {
	ID            string `json:"id"`
	SessionID     string `json:"sessionID"`
	TabID         string `json:"tabID"`
	WindowID      string `json:"windowID"`
	URL           string `json:"url"`
	Filename      string `json:"filename"`
	State         string `json:"state"`
	ReceivedBytes int64  `json:"receivedBytes"`
	TotalBytes    int64  `json:"totalBytes"`
	Interrupted   bool   `json:"interrupted,omitempty"`
	Path          string `json:"path,omitempty"`
	Error         string `json:"error,omitempty"`
	StartedAt     string `json:"startedAt"`
	FinishedAt    string `json:"finishedAt,omitempty"`
}

type DownloadResult struct {
	Downloads []Download `json:"downloads"`
}
