package notification

import (
	"crypto/md5"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// ExtractDeviceInfo extracts device information from HTTP request headers
func ExtractDeviceInfo(r *http.Request) *DeviceInfo {
	userAgent := r.Header.Get("User-Agent")
	if userAgent == "" {
		return nil
	}

	deviceInfo := &DeviceInfo{
		UserAgent:  userAgent,
		DeviceType: detectDeviceType(userAgent),
		Browser:    detectBrowser(userAgent),
		OS:         detectOS(userAgent),
	}

	// Generate device hash based on User-Agent and other headers
	deviceInfo.DeviceHash = generateDeviceHash(r)

	return deviceInfo
}

// detectDeviceType determines device type from user agent
func detectDeviceType(userAgent string) string {
	ua := strings.ToLower(userAgent)

	if strings.Contains(ua, "mobile") || strings.Contains(ua, "android") || strings.Contains(ua, "iphone") {
		return "mobile"
	}
	if strings.Contains(ua, "tablet") || strings.Contains(ua, "ipad") {
		return "tablet"
	}
	return "desktop"
}

// detectBrowser determines browser from user agent
func detectBrowser(userAgent string) string {
	ua := strings.ToLower(userAgent)

	if strings.Contains(ua, "chrome") && !strings.Contains(ua, "chromium") && !strings.Contains(ua, "edg") {
		return "chrome"
	}
	if strings.Contains(ua, "firefox") {
		return "firefox"
	}
	if strings.Contains(ua, "safari") && !strings.Contains(ua, "chrome") {
		return "safari"
	}
	if strings.Contains(ua, "edg") {
		return "edge"
	}
	if strings.Contains(ua, "opera") || strings.Contains(ua, "opr") {
		return "opera"
	}
	return "unknown"
}

// detectOS determines operating system from user agent
func detectOS(userAgent string) string {
	ua := strings.ToLower(userAgent)

	if strings.Contains(ua, "windows") {
		return "windows"
	}
	if strings.Contains(ua, "macintosh") || strings.Contains(ua, "mac os") {
		return "macos"
	}
	if strings.Contains(ua, "linux") && !strings.Contains(ua, "android") {
		return "linux"
	}
	if strings.Contains(ua, "android") {
		return "android"
	}
	if strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") || strings.Contains(ua, "ipod") {
		return "ios"
	}
	return "unknown"
}

// generateDeviceHash creates a hash to identify the device
func generateDeviceHash(r *http.Request) string {
	// Combine various headers to create a device fingerprint
	fingerprint := fmt.Sprintf("%s|%s|%s|%s",
		r.Header.Get("User-Agent"),
		r.Header.Get("Accept-Language"),
		r.Header.Get("Accept-Encoding"),
		r.RemoteAddr,
	)

	// Remove port from RemoteAddr for consistency
	re := regexp.MustCompile(`:\d+$`)
	fingerprint = re.ReplaceAllString(fingerprint, "")

	// Create MD5 hash
	hash := md5.Sum([]byte(fingerprint))
	return fmt.Sprintf("%x", hash)
}
