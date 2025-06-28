package common_tools

// ResultData contains the main sections of the search results.
type ResultData struct {
	Query  QueryInfo        `json:"query"`
	Mixed  MixedInfo        `json:"mixed"`
	News   NewsResults      `json:"news"`
	Type   string           `json:"type"` // e.g., "search"
	Videos VideoResults     `json:"videos"`
	Web    WebSearchResults `json:"web"`
}

// QueryInfo holds metadata about the search query itself.
type QueryInfo struct {
	Original             string `json:"original"`
	ShowStrictWarning    bool   `json:"show_strict_warning"`
	IsNavigational       bool   `json:"is_navigational"`
	IsNewsBreaking       bool   `json:"is_news_breaking"`
	SpellcheckOff        bool   `json:"spellcheck_off"`
	Country              string `json:"country"`
	BadResults           bool   `json:"bad_results"`
	ShouldFallback       bool   `json:"should_fallback"`
	PostalCode           string `json:"postal_code"`
	City                 string `json:"city"`
	HeaderCountry        string `json:"header_country"`
	MoreResultsAvailable bool   `json:"more_results_available"`
	State                string `json:"state"`
}

// MixedInfo describes the layout of mixed result types (web, news, videos etc.).
type MixedInfo struct {
	Type string      `json:"type"` // e.g., "mixed"
	Main []MixedItem `json:"main"`
	Top  []MixedItem `json:"top"`
	Side []MixedItem `json:"side"`
}

// MixedItem represents one item in the mixed layout plan.
type MixedItem struct {
	Type  string `json:"type"`  // e.g., "web", "news", "videos"
	Index int    `json:"index"` // Only present for types like "web"
	All   bool   `json:"all"`   // Indicates if all results of this type are included here
}

// NewsResults contains the news articles found.
type NewsResults struct {
	Type             string        `json:"type"` // e.g., "news"
	Results          []NewsArticle `json:"results"`
	MutatedByGoggles bool          `json:"mutated_by_goggles"`
}

// NewsArticle represents a single news result item.
type NewsArticle struct {
	Title          string    `json:"title"`
	URL            string    `json:"url"`
	IsSourceLocal  bool      `json:"is_source_local"`
	IsSourceBoth   bool      `json:"is_source_both"`
	Description    string    `json:"description"`
	PageAge        string    `json:"page_age,omitempty"` // ISO 8601 format
	Profile        Profile   `json:"profile"`
	FamilyFriendly bool      `json:"family_friendly"`
	MetaURL        MetaURL   `json:"meta_url"`
	Breaking       bool      `json:"breaking"`
	IsLive         bool      `json:"is_live"`
	Thumbnail      Thumbnail `json:"thumbnail"`
	Age            string    `json:"age"` // e.g., "9 hours ago"
}

// VideoResults contains the video results found.
type VideoResults struct {
	Type             string        `json:"type"` // e.g., "videos"
	Results          []VideoResult `json:"results"`
	MutatedByGoggles bool          `json:"mutated_by_goggles"`
}

// VideoResult represents a single video result item.
type VideoResult struct {
	Type        string    `json:"type"` // e.g., "video_result"
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Age         string    `json:"age,omitempty"`      // e.g., "2 weeks ago"
	PageAge     string    `json:"page_age,omitempty"` // ISO 8601 format
	Video       VideoInfo `json:"video"`
	MetaURL     MetaURL   `json:"meta_url"`
	Thumbnail   Thumbnail `json:"thumbnail"`
}

// VideoInfo contains details specific to a video.
type VideoInfo struct {
	Duration  string `json:"duration,omitempty"` // e.g., "01:02"
	Creator   string `json:"creator,omitempty"`
	Publisher string `json:"publisher,omitempty"`
	Views     *int64 `json:"views,omitempty"` // Use pointer for optional number
}

// WebSearchResults contains the organic web results.
type WebSearchResults struct {
	Type           string      `json:"type"` // e.g., "search"
	Results        []WebResult `json:"results"`
	FamilyFriendly bool        `json:"family_friendly"`
}

// WebResult represents a single organic web search result item.
type WebResult struct {
	Title          string        `json:"title"`
	URL            string        `json:"url"`
	IsSourceLocal  bool          `json:"is_source_local"`
	IsSourceBoth   bool          `json:"is_source_both"`
	Description    string        `json:"description"`
	PageAge        string        `json:"page_age,omitempty"` // ISO 8601 format
	Profile        Profile       `json:"profile"`
	Language       string        `json:"language"`
	FamilyFriendly bool          `json:"family_friendly"`
	Type           string        `json:"type"`    // e.g., "search_result"
	Subtype        string        `json:"subtype"` // e.g., "generic", "faq", "video", "article"
	IsLive         bool          `json:"is_live"`
	MetaURL        MetaURL       `json:"meta_url"`
	Thumbnail      Thumbnail     `json:"thumbnail,omitempty"`    // Thumbnails might be optional for web results
	Age            string        `json:"age,omitempty"`          // e.g., "January 20, 2025"
	ClusterType    string        `json:"cluster_type,omitempty"` // e.g., "generic"
	Cluster        []ClusterItem `json:"cluster,omitempty"`
	DeepResults    *DeepResults  `json:"deep_results,omitempty"` // Use pointer for optional object
}

// ClusterItem represents a link within a cluster (like sitelinks).
type ClusterItem struct {
	Title          string `json:"title"`
	URL            string `json:"url"`
	IsSourceLocal  bool   `json:"is_source_local"`
	IsSourceBoth   bool   `json:"is_source_both"`
	Description    string `json:"description"`
	FamilyFriendly bool   `json:"family_friendly"`
}

// DeepResults contains additional structured data like buttons.
type DeepResults struct {
	Buttons []DeepResultButton `json:"buttons"`
}

// DeepResultButton represents a button shown in deep results.
type DeepResultButton struct {
	Type  string `json:"type"` // e.g., "button_result"
	Title string `json:"title"`
	URL   string `json:"url"`
}

// --- Common Reusable Sub-structs ---

// Profile contains information about the source/publisher.
type Profile struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	LongName string `json:"long_name"`
	Img      string `json:"img"` // URL to profile image/icon
}

// MetaURL contains parsed components of a URL.
type MetaURL struct {
	Scheme   string `json:"scheme"`
	Netloc   string `json:"netloc"`
	Hostname string `json:"hostname"`
	Favicon  string `json:"favicon"` // URL to favicon
	Path     string `json:"path"`
}

// Thumbnail contains URLs for a thumbnail image.
type Thumbnail struct {
	Src      string `json:"src"`            // URL to the displayed thumbnail
	Original string `json:"original"`       // URL to the original image source
	Logo     bool   `json:"logo,omitempty"` // Indicates if the thumbnail is a logo (seen in web results)
}

// SimplifiedResultData contains the simplified sections of the search results.
type SimplifiedResultData struct {
	Query SimplifiedQueryInfo        `json:"query"`
	News  SimplifiedNewsResults      `json:"news"`
	Web   SimplifiedWebSearchResults `json:"web"`
	// Type string // You might want to keep this top-level type ('search') for context, or remove it.
}

// SimplifiedQueryInfo holds only the essential query metadata.
type SimplifiedQueryInfo struct {
	Original string `json:"original"`
	Country  string `json:"country"`
}

// SimplifiedNewsResults contains the simplified news articles.
type SimplifiedNewsResults struct {
	// Type string // Keep "news" type? Optional.
	Results []SimplifiedNewsArticle `json:"results"`
}

// SimplifiedNewsArticle represents a single simplified news result item.
type SimplifiedNewsArticle struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	PageAge     string `json:"page_age,omitempty"` // ISO 8601 format, keep if useful for LLM freshness
	Age         string `json:"age,omitempty"`      // e.g., "9 hours ago", keep if useful for LLM freshness
}

// SimplifiedWebSearchResults contains the simplified organic web results.
type SimplifiedWebSearchResults struct {
	// Type string // Keep "search" type? Optional.
	Results []SimplifiedWebResult `json:"results"`
}

// SimplifiedWebResult represents a single simplified organic web search result item.
type SimplifiedWebResult struct {
	Title       string                  `json:"title"`
	URL         string                  `json:"url"`
	Description string                  `json:"description"`
	PageAge     string                  `json:"page_age,omitempty"` // Optional: Keep if useful for LLM
	Age         string                  `json:"age,omitempty"`      // Optional: Keep if useful for LLM
	Cluster     []SimplifiedClusterItem `json:"cluster,omitempty"`  // Keep simplified cluster
}

// SimplifiedClusterItem represents a simplified link within a cluster (like sitelinks).
type SimplifiedClusterItem struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}
