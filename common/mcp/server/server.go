package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type WttrResponse struct {
	CurrentCondition []struct {
		TempC         string `json:"temp_c"`
		Humidity      string `json:"humidity"`
		WindspeedKmph string `json:"windspeedKmph"`
		WeatherDesc   []struct {
			Value string `json:"value"`
		} `json:"weatherDesc"`
	} `json:"current_condition"`

	NearestArea []struct {
		AreaName []struct {
			Value string `json:"value"`
		} `json:"areaName"`
	} `json:"nearest_area"`
}

type WeatherResponse struct {
	Location    string  `json:"location"`
	Temperature float64 `json:"temperature"`
	Condition   string  `json:"condition"`
	Humidity    int     `json:"humidity"`
	WindSpeed   float64 `json:"windSpeed"`
}

type openMeteoGeoResult struct {
	Name        string  `json:"name"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Admin1      string  `json:"admin1"`
	Country     string  `json:"country"`
	CountryCode string  `json:"country_code"`
	Population  float64 `json:"population"`
}

type openMeteoGeoResponse struct {
	Results []openMeteoGeoResult `json:"results"`
}

type openMeteoForecastResponse struct {
	Current struct {
		Temperature2M     float64 `json:"temperature_2m"`
		RelativeHumidity2 float64 `json:"relative_humidity_2m"`
		WindSpeed10M      float64 `json:"wind_speed_10m"`
		WeatherCode       int     `json:"weather_code"`
	} `json:"current"`
}

type openMeteoDailyForecastResponse struct {
	Daily struct {
		Time                     []string  `json:"time"`
		WeatherCode              []int     `json:"weather_code"`
		Temperature2MMin         []float64 `json:"temperature_2m_min"`
		Temperature2MMax         []float64 `json:"temperature_2m_max"`
		PrecipitationProbability []float64 `json:"precipitation_probability_mean"`
	} `json:"daily"`
}

type duckTopic struct {
	Text          string      `json:"Text"`
	FirstURL      string      `json:"FirstURL"`
	Result        string      `json:"Result"`
	RelatedTopics []duckTopic `json:"RelatedTopics"`
}

type duckSearchResponse struct {
	Heading       string      `json:"Heading"`
	AbstractText  string      `json:"AbstractText"`
	AbstractURL   string      `json:"AbstractURL"`
	RelatedTopics []duckTopic `json:"RelatedTopics"`
}

type WeatherAPIClient struct {
	client *http.Client
}

func NewWeatherAPIClient() *WeatherAPIClient {
	return &WeatherAPIClient{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *WeatherAPIClient) GetWeather(ctx context.Context, city string) (*WeatherResponse, error) {
	city = strings.TrimSpace(city)
	if city == "" {
		return nil, fmt.Errorf("city is required")
	}
	// Use geocoding + Open-Meteo as the single source of truth.
	// This avoids wttr returning caller-IP fallback weather for invalid locations.
	return c.getWeatherFromOpenMeteo(ctx, city)
}

func (c *WeatherAPIClient) GetWeatherForecast(ctx context.Context, city string, days int) (string, error) {
	city = strings.TrimSpace(city)
	if city == "" {
		return "", fmt.Errorf("city is required")
	}
	days = normalizeForecastDays(days)

	geo, err := c.geocodeCity(ctx, city)
	if err != nil {
		return "", err
	}

	forecastURL := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.6f&longitude=%.6f&daily=weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_mean&timezone=auto&forecast_days=%d",
		geo.Latitude, geo.Longitude, days,
	)

	var fc openMeteoDailyForecastResponse
	if err := c.fetchJSON(ctx, forecastURL, &fc); err != nil {
		return "", fmt.Errorf("forecast failed: %w", err)
	}
	if len(fc.Daily.Time) == 0 {
		return "", fmt.Errorf("no forecast data")
	}

	location := geo.Name
	if strings.TrimSpace(geo.Admin1) != "" {
		location += ", " + geo.Admin1
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Forecast for %s (%d days)\n", location, days))
	for i := 0; i < len(fc.Daily.Time) && i < days; i++ {
		code := 0
		if i < len(fc.Daily.WeatherCode) {
			code = fc.Daily.WeatherCode[i]
		}
		maxT, minT, pop := 0.0, 0.0, 0.0
		if i < len(fc.Daily.Temperature2MMax) {
			maxT = fc.Daily.Temperature2MMax[i]
		}
		if i < len(fc.Daily.Temperature2MMin) {
			minT = fc.Daily.Temperature2MMin[i]
		}
		if i < len(fc.Daily.PrecipitationProbability) {
			pop = fc.Daily.PrecipitationProbability[i]
		}

		b.WriteString(fmt.Sprintf(
			"%s | %s | %.1f~%.1f°C | Precip: %.0f%%\n",
			fc.Daily.Time[i],
			weatherCodeToText(code),
			minT,
			maxT,
			pop,
		))
	}
	return strings.TrimSpace(b.String()), nil
}

func (c *WeatherAPIClient) getWeatherFromOpenMeteo(ctx context.Context, city string) (*WeatherResponse, error) {
	geo, err := c.geocodeCity(ctx, city)
	if err != nil {
		return nil, err
	}
	weatherURL := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.6f&longitude=%.6f&current=temperature_2m,relative_humidity_2m,wind_speed_10m,weather_code&timezone=auto",
		geo.Latitude, geo.Longitude,
	)

	var forecastResp openMeteoForecastResponse
	if err := c.fetchJSON(ctx, weatherURL, &forecastResp); err != nil {
		return nil, fmt.Errorf("open-meteo forecast failed: %w", err)
	}

	location := geo.Name
	if strings.TrimSpace(geo.Admin1) != "" {
		location += ", " + geo.Admin1
	}
	if strings.TrimSpace(geo.Country) != "" {
		location += ", " + geo.Country
	}

	return &WeatherResponse{
		Location:    location,
		Temperature: forecastResp.Current.Temperature2M,
		Condition:   weatherCodeToText(forecastResp.Current.WeatherCode),
		Humidity:    int(math.Round(forecastResp.Current.RelativeHumidity2)),
		WindSpeed:   forecastResp.Current.WindSpeed10M,
	}, nil
}

func (c *WeatherAPIClient) geocodeCity(ctx context.Context, city string) (*openMeteoGeoResult, error) {
	city = strings.TrimSpace(city)
	if city == "" {
		return nil, fmt.Errorf("city is required")
	}

	queries := []string{city}
	if !strings.HasSuffix(city, "市") {
		queries = append(queries, city+"市")
	}

	candidates := make([]openMeteoGeoResult, 0, 20)
	seen := make(map[string]struct{})

	for _, q := range queries {
		geoURL := fmt.Sprintf(
			"https://geocoding-api.open-meteo.com/v1/search?name=%s&count=10&language=zh&format=json",
			url.QueryEscape(q),
		)

		var geoResp openMeteoGeoResponse
		if err := c.fetchJSON(ctx, geoURL, &geoResp); err != nil {
			continue
		}
		for _, r := range geoResp.Results {
			key := fmt.Sprintf("%s|%s|%s|%.5f|%.5f", r.Name, r.Admin1, r.Country, r.Latitude, r.Longitude)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, r)
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no geocoding result for city=%s", city)
	}

	best := candidates[0]
	bestScore := geoScore(city, best)
	for i := 1; i < len(candidates); i++ {
		score := geoScore(city, candidates[i])
		if score > bestScore {
			best = candidates[i]
			bestScore = score
		}
	}

	return &best, nil
}

func geoScore(city string, r openMeteoGeoResult) int {
	cityNorm := normalizeLocation(city)
	nameNorm := normalizeLocation(r.Name)
	adminNorm := normalizeLocation(r.Admin1)
	countryNorm := normalizeLocation(r.Country)

	score := 0

	if nameNorm == cityNorm {
		score += 60
	}
	if adminNorm == cityNorm {
		score += 35
	}
	if strings.Contains(nameNorm, cityNorm) || strings.Contains(cityNorm, nameNorm) {
		score += 20
	}
	if strings.Contains(adminNorm, cityNorm) {
		score += 15
	}
	if strings.EqualFold(r.CountryCode, "CN") || strings.Contains(countryNorm, "中国") || strings.Contains(countryNorm, "china") {
		score += 20
	}

	// Prefer matching municipality for ambiguous names like 北京/重庆.
	if target, ok := canonicalMunicipality(cityNorm); ok {
		if adminCity, ok2 := canonicalMunicipality(adminNorm); ok2 {
			if adminCity == target {
				score += 40
			} else {
				score -= 40
			}
		}
		if nameCity, ok2 := canonicalMunicipality(nameNorm); ok2 {
			if nameCity == target {
				score += 25
			}
		}
	}

	if r.Population > 0 {
		// Use population as weak tie-breaker.
		popBonus := int(r.Population / 1000000)
		if popBonus > 20 {
			popBonus = 20
		}
		score += popBonus
	}

	return score
}

func normalizeLocation(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, " ", "")
	replacer := strings.NewReplacer(
		"中华人民共和国", "",
		"特别行政区", "",
		"自治区", "",
		"自治州", "",
		"省", "",
		"市", "",
		"地区", "",
		"盟", "",
		"county", "",
		"city", "",
		"province", "",
	)
	return replacer.Replace(s)
}

func canonicalMunicipality(s string) (string, bool) {
	v := strings.TrimSpace(strings.ToLower(s))
	switch {
	case strings.Contains(v, "北京"), strings.Contains(v, "beijing"):
		return "beijing", true
	case strings.Contains(v, "上海"), strings.Contains(v, "shanghai"):
		return "shanghai", true
	case strings.Contains(v, "天津"), strings.Contains(v, "tianjin"):
		return "tianjin", true
	case strings.Contains(v, "重庆"), strings.Contains(v, "chongqing"):
		return "chongqing", true
	default:
		return "", false
	}
}

func (c *WeatherAPIClient) fetchJSON(ctx context.Context, requestURL string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("User-Agent", "ai-chat-mcp-weather/1.0")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Connection", "close")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("json parse failed: %w", err)
	}
	return nil
}

func (c *WeatherAPIClient) WebSearch(ctx context.Context, query string, limit int) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	limit = clamp(limit, 1, 10)
	searchQuery := normalizeSearchQuery(query)
	backendLimit := clamp(limit*4, 6, 30)
	errs := make([]string, 0, 4)

	text, err := c.searchWithDuckDuckGo(ctx, searchQuery, backendLimit)
	if err == nil {
		if finalText, ok := finalizeSearchOutput(query, text, limit); ok {
			return finalText, nil
		}
		err = fmt.Errorf("no reliable result after ranking")
	}
	errs = append(errs, "duckduckgo-json="+err.Error())
	log.Printf("duckduckgo json search failed, fallback to duckduckgo html: %v", err)

	text, err = c.searchWithDuckDuckGoHTML(ctx, searchQuery, backendLimit)
	if err == nil {
		if finalText, ok := finalizeSearchOutput(query, text, limit); ok {
			return finalText, nil
		}
		err = fmt.Errorf("no reliable result after ranking")
	}
	errs = append(errs, "duckduckgo-html="+err.Error())
	log.Printf("duckduckgo html search failed, fallback to bing: %v", err)

	text, err = c.searchWithBing(ctx, searchQuery, backendLimit)
	if err == nil {
		if finalText, ok := finalizeSearchOutput(query, text, limit); ok {
			return finalText, nil
		}
		err = fmt.Errorf("no reliable result after ranking")
	}
	errs = append(errs, "bing="+err.Error())
	log.Printf("bing search failed, fallback to baidu: %v", err)

	text, err = c.searchWithBaidu(ctx, searchQuery, backendLimit)
	if err == nil {
		if finalText, ok := finalizeSearchOutput(query, text, limit); ok {
			return finalText, nil
		}
		err = fmt.Errorf("no reliable result after ranking")
	}
	errs = append(errs, "baidu="+err.Error())

	return "", fmt.Errorf("web search failed: %s", strings.Join(errs, "; "))
}

type webSearchItem struct {
	Title string
	URL   string
}

func normalizeSearchQuery(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return q
	}
	if !isOfficialSiteIntent(q) {
		return q
	}
	brand := extractOfficialBrand(q)
	if brand == "" {
		return q
	}
	return brand + " 官网 官方网站"
}

func finalizeSearchOutput(rawQuery, rawText string, limit int) (string, bool) {
	items := parseSearchItems(rawText)
	if len(items) == 0 {
		return "", false
	}

	officialIntent := isOfficialSiteIntent(rawQuery)
	brand := extractOfficialBrand(rawQuery)

	scored := make([]struct {
		item  webSearchItem
		score int
	}, 0, len(items))

	for i, it := range items {
		score := rankSearchItem(it, rawQuery, brand, officialIntent, i)
		if officialIntent && score < 0 {
			continue
		}
		scored = append(scored, struct {
			item  webSearchItem
			score int
		}{item: it, score: score})
	}

	if len(scored) == 0 {
		return "", false
	}

	// Simple stable sort by score desc.
	for i := 0; i < len(scored)-1; i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	maxN := limit
	if maxN > len(scored) {
		maxN = len(scored)
	}
	if maxN == 0 {
		return "", false
	}

	var b strings.Builder
	for i := 0; i < maxN; i++ {
		b.WriteString(fmt.Sprintf("%d. %s | %s", i+1, scored[i].item.Title, scored[i].item.URL))
		if i != maxN-1 {
			b.WriteString("\n")
		}
	}
	return b.String(), true
}

func parseSearchItems(rawText string) []webSearchItem {
	lines := strings.Split(rawText, "\n")
	items := make([]webSearchItem, 0, len(lines))
	seen := make(map[string]struct{})
	reURL := regexp.MustCompile(`https?://[^\s|]+`)
	reNumPrefix := regexp.MustCompile(`^\s*\d+\.\s*`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		urls := reURL.FindAllString(line, -1)
		if len(urls) == 0 {
			continue
		}
		link := strings.TrimSpace(urls[0])
		u, err := url.Parse(link)
		if err != nil || u.Host == "" {
			continue
		}
		link = u.String()
		if _, ok := seen[link]; ok {
			continue
		}
		seen[link] = struct{}{}

		title := line
		if idx := strings.Index(line, "|"); idx > 0 {
			title = strings.TrimSpace(line[:idx])
		}
		title = reNumPrefix.ReplaceAllString(title, "")
		title = reURL.ReplaceAllString(title, "")
		title = strings.TrimSpace(title)
		if title == "" {
			title = u.Host
		}

		items = append(items, webSearchItem{
			Title: title,
			URL:   link,
		})
	}
	return items
}

func rankSearchItem(item webSearchItem, rawQuery, brand string, officialIntent bool, index int) int {
	score := 100 - index
	u, err := url.Parse(item.URL)
	if err != nil {
		return -999
	}
	host := strings.ToLower(u.Hostname())
	title := strings.ToLower(item.Title)
	query := strings.ToLower(strings.TrimSpace(rawQuery))
	brandLower := strings.ToLower(strings.TrimSpace(brand))

	if host == "" {
		return -999
	}

	if officialIntent {
		if isLowQualityOfficialDomain(host) {
			score -= 220
		}
		if strings.Contains(title, "官网") || strings.Contains(title, "官方网站") || strings.Contains(title, "official") {
			score += 55
		}
		if u.Path == "" || u.Path == "/" {
			score += 20
		}
		if len(strings.Trim(u.Path, "/")) > 24 {
			score -= 12
		}

		if brandLower != "" {
			if strings.Contains(strings.ToLower(item.Title), brandLower) {
				score += 24
			}
			if strings.Contains(host, strings.ReplaceAll(brandLower, " ", "")) {
				score += 30
			}
			for _, hint := range officialDomainHints(brandLower) {
				if host == hint || strings.HasSuffix(host, "."+hint) {
					score += 120
				}
			}
		}

		if strings.Contains(query, "天气") {
			if strings.Contains(host, "weather.com.cn") {
				score += 90
			}
			if strings.Contains(host, "cma.cn") || strings.Contains(host, "nmc.cn") {
				score += 70
			}
		}
	}

	return score
}

func isOfficialSiteIntent(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	return strings.Contains(q, "官网") ||
		strings.Contains(q, "官方网站") ||
		strings.Contains(q, "official site") ||
		strings.Contains(q, "official website")
}

func extractOfficialBrand(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}
	re := regexp.MustCompile(`^\s*(?:帮我)?(?:搜索|查|找)?(?:一下)?\s*([\p{Han}A-Za-z0-9·\-\s]{1,32}?)(?:的)?(?:官网|官方网站)`)
	if m := re.FindStringSubmatch(q); len(m) >= 2 {
		v := strings.TrimSpace(m[1])
		v = strings.Trim(v, "，。,.?？:：!！")
		return v
	}

	idx := strings.Index(q, "官网")
	if idx > 0 {
		v := strings.TrimSpace(q[:idx])
		v = strings.TrimPrefix(v, "帮我")
		v = strings.TrimPrefix(v, "搜索")
		v = strings.TrimPrefix(v, "查一下")
		v = strings.TrimPrefix(v, "查")
		v = strings.TrimPrefix(v, "找一下")
		v = strings.Trim(v, " 的")
		v = strings.Trim(v, "，。,.?？:：!！")
		return strings.TrimSpace(v)
	}
	return ""
}

func isLowQualityOfficialDomain(host string) bool {
	blocked := []string{
		"zhihu.com",
		"zhidao.baidu.com",
		"baike.baidu.com",
		"tieba.baidu.com",
		"weibo.com",
		"bilibili.com",
		"douyin.com",
		"xiaohongshu.com",
		"csdn.net",
		"toutiao.com",
		"sohu.com",
		"163.com",
		"qq.com",
	}
	for _, b := range blocked {
		if host == b || strings.HasSuffix(host, "."+b) {
			return true
		}
	}
	return false
}

func officialDomainHints(brandLower string) []string {
	hints := map[string][]string{
		"小米":        {"xiaomi.com", "mi.com"},
		"xiaomi":    {"xiaomi.com", "mi.com"},
		"华为":        {"huawei.com"},
		"huawei":    {"huawei.com"},
		"腾讯":        {"tencent.com", "qq.com"},
		"tencent":   {"tencent.com"},
		"阿里":        {"alibaba.com"},
		"阿里巴巴":      {"alibaba.com"},
		"alibaba":   {"alibaba.com"},
		"字节":        {"bytedance.com"},
		"字节跳动":      {"bytedance.com"},
		"bytedance": {"bytedance.com"},
		"百度":        {"baidu.com"},
		"baidu":     {"baidu.com"},
		"京东":        {"jd.com"},
		"jd":        {"jd.com"},
		"美团":        {"meituan.com"},
		"meituan":   {"meituan.com"},
	}
	for k, v := range hints {
		if strings.Contains(brandLower, k) {
			return v
		}
	}
	return nil
}

func (c *WeatherAPIClient) searchWithDuckDuckGo(ctx context.Context, query string, limit int) (string, error) {
	apiURL := fmt.Sprintf(
		"https://api.duckduckgo.com/?q=%s&format=json&no_redirect=1&no_html=1&skip_disambig=1",
		url.QueryEscape(query),
	)

	var resp duckSearchResponse
	if err := c.fetchJSON(ctx, apiURL, &resp); err != nil {
		return "", err
	}

	lines := make([]string, 0, limit+1)
	if strings.TrimSpace(resp.AbstractText) != "" {
		lines = append(lines, fmt.Sprintf("1. %s | %s", strings.TrimSpace(resp.AbstractText), strings.TrimSpace(resp.AbstractURL)))
	}

	var walk func([]duckTopic)
	walk = func(items []duckTopic) {
		for _, it := range items {
			if len(lines) >= limit {
				return
			}
			if strings.TrimSpace(it.Text) != "" {
				link := strings.TrimSpace(it.FirstURL)
				lines = append(lines, fmt.Sprintf("%d. %s | %s", len(lines)+1, strings.TrimSpace(it.Text), link))
			}
			if len(it.RelatedTopics) > 0 {
				walk(it.RelatedTopics)
			}
		}
	}
	walk(resp.RelatedTopics)

	if len(lines) == 0 {
		return "", fmt.Errorf("no search result")
	}
	return strings.Join(lines, "\n"), nil
}

func (c *WeatherAPIClient) searchWithDuckDuckGoHTML(ctx context.Context, query string, limit int) (string, error) {
	searchURL := fmt.Sprintf("https://duckduckgo.com/html/?q=%s&kl=cn-zh", url.QueryEscape(query))
	htmlText, err := c.fetchHTMLWithTimeout(ctx, searchURL, 8*time.Second)
	if err != nil {
		return "", err
	}

	re := regexp.MustCompile(`(?is)<a[^>]*class="[^"]*result__a[^"]*"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	matches := re.FindAllStringSubmatch(htmlText, limit*3)
	if len(matches) == 0 {
		return "", fmt.Errorf("no parseable result from duckduckgo html")
	}

	lines := make([]string, 0, limit)
	for _, m := range matches {
		if len(lines) >= limit {
			break
		}
		if len(m) < 3 {
			continue
		}
		link := normalizeSearchResultLink("https://duckduckgo.com", strings.TrimSpace(html.UnescapeString(m[1])))
		link = unwrapDuckDuckGoRedirect(link)
		title := cleanHTMLText(m[2])
		if title == "" || link == "" {
			continue
		}
		if !isValidExternalLink(link, []string{"duckduckgo.com"}) {
			continue
		}
		lines = append(lines, fmt.Sprintf("%d. %s | %s", len(lines)+1, title, link))
	}

	if len(lines) == 0 {
		return "", fmt.Errorf("no valid result from duckduckgo html")
	}
	return strings.Join(lines, "\n"), nil
}

func (c *WeatherAPIClient) searchWithBing(ctx context.Context, query string, limit int) (string, error) {
	searchURL := fmt.Sprintf("https://www.bing.com/search?q=%s&setlang=zh-Hans", url.QueryEscape(query))
	htmlText, err := c.fetchHTMLWithTimeout(ctx, searchURL, 8*time.Second)
	if err != nil {
		return "", err
	}

	lines := extractSearchAnchors(
		"https://www.bing.com",
		htmlText,
		limit,
		[]string{"bing.com", "microsoft.com"},
	)
	if len(lines) == 0 {
		return "", fmt.Errorf("no parseable result from bing")
	}
	return strings.Join(lines, "\n"), nil
}

func (c *WeatherAPIClient) searchWithBaidu(ctx context.Context, query string, limit int) (string, error) {
	searchURL := fmt.Sprintf("https://www.baidu.com/s?wd=%s", url.QueryEscape(query))
	htmlText, err := c.fetchHTMLWithTimeout(ctx, searchURL, 8*time.Second)
	if err != nil {
		return "", err
	}

	lines := extractSearchAnchors(
		"https://www.baidu.com",
		htmlText,
		limit,
		[]string{"baidu.com"},
	)
	if len(lines) == 0 {
		return "", fmt.Errorf("no parseable result from baidu")
	}
	return strings.Join(lines, "\n"), nil
}

func cleanHTMLText(s string) string {
	reTag := regexp.MustCompile(`(?s)<[^>]+>`)
	reSpace := regexp.MustCompile(`\s+`)
	v := reTag.ReplaceAllString(s, " ")
	v = html.UnescapeString(v)
	return strings.TrimSpace(reSpace.ReplaceAllString(v, " "))
}

func (c *WeatherAPIClient) fetchHTMLWithTimeout(ctx context.Context, rawURL string, timeout time.Duration) (string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Connection", "close")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read failed: %w", err)
	}
	return string(body), nil
}

func extractSearchAnchors(baseURL, htmlText string, limit int, blockedHosts []string) []string {
	reAnchor := regexp.MustCompile(`(?is)<a[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	matches := reAnchor.FindAllStringSubmatch(htmlText, limit*50)
	if len(matches) == 0 {
		return nil
	}

	lines := make([]string, 0, limit)
	seen := make(map[string]struct{})
	for _, m := range matches {
		if len(lines) >= limit {
			break
		}
		if len(m) < 3 {
			continue
		}
		link := normalizeSearchResultLink(baseURL, strings.TrimSpace(html.UnescapeString(m[1])))
		link = unwrapDuckDuckGoRedirect(link)
		if !isValidExternalLink(link, blockedHosts) {
			continue
		}
		if _, ok := seen[link]; ok {
			continue
		}
		seen[link] = struct{}{}

		title := cleanHTMLText(m[2])
		if title == "" || len([]rune(title)) < 2 {
			continue
		}
		lines = append(lines, fmt.Sprintf("%d. %s | %s", len(lines)+1, title, link))
	}
	return lines
}

func normalizeSearchResultLink(baseURL, href string) string {
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	base, berr := url.Parse(baseURL)
	ref, rerr := url.Parse(href)
	if berr != nil || rerr != nil {
		return href
	}
	return base.ResolveReference(ref).String()
}

func unwrapDuckDuckGoRedirect(rawLink string) string {
	u, err := url.Parse(rawLink)
	if err != nil {
		return rawLink
	}
	if !strings.Contains(strings.ToLower(u.Host), "duckduckgo.com") {
		return rawLink
	}
	q := u.Query()
	if target := strings.TrimSpace(q.Get("uddg")); target != "" {
		if unescaped, err := url.QueryUnescape(target); err == nil && strings.TrimSpace(unescaped) != "" {
			return unescaped
		}
		return target
	}
	return rawLink
}

func isValidExternalLink(rawLink string, blockedHosts []string) bool {
	u, err := url.Parse(strings.TrimSpace(rawLink))
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Host)
	if host == "" {
		return false
	}
	for _, b := range blockedHosts {
		b = strings.ToLower(strings.TrimSpace(b))
		if b == "" {
			continue
		}
		if host == b || strings.HasSuffix(host, "."+b) {
			return false
		}
	}
	return true
}

func (c *WeatherAPIClient) WebFetch(ctx context.Context, rawURL string, maxChars int) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}
	maxChars = clamp(maxChars, 200, 8000)

	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid url")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("User-Agent", "ai-chat-mcp-webfetch/1.0")
	req.Header.Set("Accept", "text/html,text/plain")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read failed: %w", err)
	}
	text := string(body)

	reScript := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reTag := regexp.MustCompile(`(?s)<[^>]+>`)
	reSpace := regexp.MustCompile(`\s+`)

	text = reScript.ReplaceAllString(text, " ")
	text = reStyle.ReplaceAllString(text, " ")
	text = reTag.ReplaceAllString(text, " ")
	text = html.UnescapeString(text)
	text = strings.TrimSpace(reSpace.ReplaceAllString(text, " "))

	if text == "" {
		return "", fmt.Errorf("empty page content")
	}
	return truncateRunes(text, maxChars), nil
}

func (c *WeatherAPIClient) TranslateText(ctx context.Context, text, sourceLang, targetLang string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("text is required")
	}

	sourceLang = normalizeLangCode(sourceLang, "auto")
	targetLang = normalizeLangCode(targetLang, "zh")
	if sourceLang != "auto" && sourceLang == targetLang {
		return text, nil
	}

	translated, err := c.translateByOpenAI(ctx, text, sourceLang, targetLang)
	if err != nil {
		return "", err
	}
	translated = strings.TrimSpace(translated)
	if translated == "" {
		return "", fmt.Errorf("empty translation result")
	}
	return translated, nil
}

func (c *WeatherAPIClient) translateByOpenAI(ctx context.Context, text, sourceLang, targetLang string) (string, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return "", fmt.Errorf("OPENAI_API_KEY is empty")
	}

	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("OPEN_AI_BASE_URL"))
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	modelName := strings.TrimSpace(os.Getenv("OPENAI_MODEL_NAME"))
	if modelName == "" {
		modelName = "gpt-4o-mini"
	}

	systemPrompt := fmt.Sprintf(
		"You are a translation engine. Translate text from %s to %s. Return translation only.",
		sourceLang, targetLang,
	)
	reqBody := map[string]any{
		"model": modelName,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": text},
		},
		"temperature": 0.1,
	}

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal translation request failed: %w", err)
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("create translation request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("translation request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read translation response failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("translation status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &completion); err != nil {
		return "", fmt.Errorf("parse translation response failed: %w", err)
	}
	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("translation response has no choices")
	}
	return completion.Choices[0].Message.Content, nil
}

func normalizeLangCode(lang, fallback string) string {
	v := strings.ToLower(strings.TrimSpace(lang))
	if v == "" {
		return fallback
	}
	switch v {
	case "auto", "zh", "en", "ja", "ko", "fr", "de", "es", "ru", "it", "pt", "ar", "hi":
		return v
	case "chinese":
		return "zh"
	case "english":
		return "en"
	case "japanese":
		return "ja"
	case "korean":
		return "ko"
	case "french":
		return "fr"
	case "german":
		return "de"
	case "spanish":
		return "es"
	default:
		return v
	}
}

func weatherCodeToText(code int) string {
	switch code {
	case 0:
		return "Clear"
	case 1, 2:
		return "Partly cloudy"
	case 3:
		return "Overcast"
	case 45, 48:
		return "Fog"
	case 51, 53, 55, 56, 57:
		return "Drizzle"
	case 61, 63, 65, 66, 67, 80, 81, 82:
		return "Rain"
	case 71, 73, 75, 77, 85, 86:
		return "Snow"
	case 95, 96, 99:
		return "Thunderstorm"
	default:
		return "Unknown"
	}
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}

func normalizeForecastDays(days int) int {
	if days <= 0 {
		return 3
	}
	if days > 7 {
		return 7
	}
	return days
}

func NewMCPServer() *server.MCPServer {
	weatherClient := NewWeatherAPIClient()

	mcpServer := server.NewMCPServer(
		"weather-query-server",
		"1.1.0",
		server.WithToolCapabilities(true),
		server.WithLogging(),
	)

	parseOptionalInt := func(args map[string]any, key string, def int) int {
		raw, ok := args[key]
		if !ok {
			return def
		}
		switch v := raw.(type) {
		case float64:
			return int(v)
		case int:
			return v
		case int64:
			return int(v)
		case string:
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				return n
			}
		}
		return def
	}
	parseOptionalString := func(args map[string]any, key, def string) string {
		raw, ok := args[key]
		if !ok {
			return def
		}
		if s, ok := raw.(string); ok {
			s = strings.TrimSpace(s)
			if s != "" {
				return s
			}
		}
		return def
	}

	// 1) Current weather
	mcpServer.AddTool(
		mcp.NewTool(
			"get_weather",
			mcp.WithDescription("Get current weather for a city"),
			mcp.WithString(
				"city",
				mcp.Description("City name, e.g. Beijing or Shanghai"),
				mcp.Required(),
			),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			city, ok := args["city"].(string)
			if !ok || strings.TrimSpace(city) == "" {
				return nil, fmt.Errorf("invalid city argument")
			}

			weather, err := weatherClient.GetWeather(ctx, city)
			if err != nil {
				return nil, err
			}

			resultText := fmt.Sprintf(
				"City: %s\nTemperature: %.1f°C\nCondition: %s\nHumidity: %d%%\nWind: %.1f km/h",
				weather.Location,
				weather.Temperature,
				weather.Condition,
				weather.Humidity,
				weather.WindSpeed,
			)

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: resultText},
				},
			}, nil
		},
	)

	// 2) Forecast weather
	mcpServer.AddTool(
		mcp.NewTool(
			"get_weather_forecast",
			mcp.WithDescription("Get weather forecast for a city"),
			mcp.WithString(
				"city",
				mcp.Description("City name, e.g. Beijing"),
				mcp.Required(),
			),
			mcp.WithString(
				"days",
				mcp.Description("Forecast days, 1-7, default 3"),
			),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			city, ok := args["city"].(string)
			if !ok || strings.TrimSpace(city) == "" {
				return nil, fmt.Errorf("invalid city argument")
			}

			days := normalizeForecastDays(parseOptionalInt(args, "days", 3))
			text, err := weatherClient.GetWeatherForecast(ctx, city, days)
			if err != nil {
				return nil, err
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: text},
				},
			}, nil
		},
	)

	// 3) Web search
	mcpServer.AddTool(
		mcp.NewTool(
			"web_search",
			mcp.WithDescription("Search the web by query and return top results"),
			mcp.WithString(
				"query",
				mcp.Description("Search query"),
				mcp.Required(),
			),
			mcp.WithString(
				"limit",
				mcp.Description("Result limit, 1-10, default 5"),
			),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			query, ok := args["query"].(string)
			if !ok || strings.TrimSpace(query) == "" {
				return nil, fmt.Errorf("invalid query argument")
			}

			limit := clamp(parseOptionalInt(args, "limit", 5), 1, 10)
			text, err := weatherClient.WebSearch(ctx, query, limit)
			if err != nil {
				return nil, err
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: text},
				},
			}, nil
		},
	)

	// 4) Web fetch
	mcpServer.AddTool(
		mcp.NewTool(
			"web_fetch",
			mcp.WithDescription("Fetch and extract text content from a URL"),
			mcp.WithString(
				"url",
				mcp.Description("Target URL, must start with http/https"),
				mcp.Required(),
			),
			mcp.WithString(
				"max_chars",
				mcp.Description("Max output length, 200-8000, default 4000"),
			),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			targetURL, ok := args["url"].(string)
			if !ok || strings.TrimSpace(targetURL) == "" {
				return nil, fmt.Errorf("invalid url argument")
			}

			maxChars := clamp(parseOptionalInt(args, "max_chars", 4000), 200, 8000)
			text, err := weatherClient.WebFetch(ctx, targetURL, maxChars)
			if err != nil {
				return nil, err
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: text},
				},
			}, nil
		},
	)

	// 5) Translation
	mcpServer.AddTool(
		mcp.NewTool(
			"translate_text",
			mcp.WithDescription("Translate text between languages"),
			mcp.WithString(
				"text",
				mcp.Description("Input text to translate"),
				mcp.Required(),
			),
			mcp.WithString(
				"source_lang",
				mcp.Description("Source language code, e.g. auto|zh|en"),
			),
			mcp.WithString(
				"target_lang",
				mcp.Description("Target language code, e.g. zh|en|ja"),
				mcp.Required(),
			),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			text, ok := args["text"].(string)
			if !ok || strings.TrimSpace(text) == "" {
				return nil, fmt.Errorf("invalid text argument")
			}

			sourceLang := parseOptionalString(args, "source_lang", "auto")
			targetLang := parseOptionalString(args, "target_lang", "zh")
			result, err := weatherClient.TranslateText(ctx, text, sourceLang, targetLang)
			if err != nil {
				return nil, err
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.TextContent{Type: "text", Text: result},
				},
			}, nil
		},
	)

	return mcpServer
}

// StartServer starts MCP HTTP server.
func StartServer(httpAddr string) error {
	mcpServer := NewMCPServer()

	httpServer := server.NewStreamableHTTPServer(mcpServer)
	log.Printf("HTTP MCP server listening on %s/mcp", httpAddr)
	return httpServer.Start(httpAddr)
}
