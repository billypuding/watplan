package wat

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"strings"
	"time"
)

const defaultProxyPause = 30 * time.Minute

var errProxySwitched = errors.New("przełączono na zapasowe proxy")

func (f *Fetcher) currentProxy() *url.URL {
	f.proxyMu.Lock()
	defer f.proxyMu.Unlock()
	if len(f.proxies) == 0 {
		return nil
	}
	if f.active != 0 && time.Now().After(f.proxyBad[0]) {
		f.active = 0
	}
	return f.proxies[f.active]
}

func (f *Fetcher) proxyPaused() (time.Time, bool) {
	f.proxyMu.Lock()
	defer f.proxyMu.Unlock()
	if len(f.proxies) == 0 {
		return time.Time{}, false
	}
	now := time.Now()
	var soonest time.Time
	for _, until := range f.proxyBad {
		if !now.Before(until) {
			return time.Time{}, false
		}
		if soonest.IsZero() || until.Before(soonest) {
			soonest = until
		}
	}
	return soonest, true
}

func (f *Fetcher) alternateProxyLocked(used *url.URL, now time.Time) int {
	for i, p := range f.proxies {
		if p != used && now.After(f.proxyBad[i]) {
			return i
		}
	}
	return -1
}

func (f *Fetcher) markProxyBadLocked(used *url.URL, now time.Time) {
	for i, p := range f.proxies {
		if p == used {
			f.proxyBad[i] = now.Add(f.proxyPause)
			return
		}
	}
}

func (f *Fetcher) proxyFailed(used *url.URL, status string) error {
	f.proxyMu.Lock()
	now := time.Now()
	f.proxyError, f.proxyAt = used.Host+" "+status, now
	f.markProxyBadLocked(used, now)
	next := f.alternateProxyLocked(used, now)
	if next >= 0 {
		f.active = next
	}
	f.proxyMu.Unlock()

	if next >= 0 {
		backup := f.proxies[next].Host
		log.Printf("proxy %s odrzuciło połączenie: %s - przełączam na %s", used.Host, status, backup)
		f.alert.Problem("proxy:"+used.Host, fmt.Sprintf("Proxy %s odrzuca połączenia: %s. Przełączyłem na zapasowe %s. Sprawdź konto u dostawcy.", used.Host, status, backup))
		return fmt.Errorf("%w: %s", errProxySwitched, backup)
	}
	log.Printf("proxy %s odrzuciło połączenie: %s - brak działającego proxy, wstrzymuję pobieranie na %v", used.Host, status, f.proxyPause)
	f.alert.Problem("proxy", fmt.Sprintf("Żadne proxy nie działa (ostatnie: %s, %s). Pobieranie z WAT stoi. Sprawdź konta u dostawców proxy.", used.Host, status))
	return fmt.Errorf("%w: %s %s - sprawdź konto u dostawcy proxy", ErrProxy, used.Host, status)
}

func (f *Fetcher) proxyBlockedByWAF(used *url.URL, host, reason string) error {
	f.proxyMu.Lock()
	now := time.Now()
	f.proxyError, f.proxyAt = fmt.Sprintf("%s WAT %s", used.Host, reason), now
	next := f.alternateProxyLocked(used, now)
	if next >= 0 {
		f.markProxyBadLocked(used, now)
		f.active = next
	}
	f.proxyMu.Unlock()

	if next >= 0 {
		backup := f.proxies[next].Host
		log.Printf("proxy %s zablokowane przez WAT (%s) - przełączam na %s", used.Host, reason, backup)
		f.alert.Problem("proxy:"+used.Host, fmt.Sprintf("WAT blokuje proxy %s (%s). Przełączyłem na zapasowe %s.", used.Host, reason, backup))
		return fmt.Errorf("%w: %s", errProxySwitched, backup)
	}

	log.Printf("proxy %s zablokowane przez WAT (%s) - brak zapasowego proxy, wstrzymuję %s", used.Host, reason, host)
	return fmt.Errorf("%w (%s, proxy %s)", ErrBlocked, reason, used.Host)
}

func (f *Fetcher) proxyTimedOut(used *url.URL, host string, cause error) error {
	f.proxyMu.Lock()
	now := time.Now()
	f.proxyError, f.proxyAt = used.Host+" timeout", now
	next := f.alternateProxyLocked(used, now)
	if next >= 0 {
		f.markProxyBadLocked(used, now)
		f.active = next
	}
	f.proxyMu.Unlock()

	if next >= 0 {
		backup := f.proxies[next].Host
		log.Printf("proxy %s nie odpowiedziało w czasie - przełączam na %s", used.Host, backup)
		f.alert.Problem("proxy:"+used.Host, fmt.Sprintf("Proxy %s nie odpowiada. Przełączyłem na zapasowe %s.", used.Host, backup))
		return fmt.Errorf("%w: %s", errProxySwitched, backup)
	}

	log.Printf("proxy %s nie odpowiedziało w czasie - brak zapasowego proxy, wstrzymuję %s", used.Host, host)
	return fmt.Errorf("%w: proxy %s nie odpowiedziało w czasie (%v)", ErrBlocked, used.Host, cause)
}

func (f *Fetcher) proxyWorked(used *url.URL) {
	f.proxyMu.Lock()
	now := time.Now()
	for i, p := range f.proxies {
		if p == used {
			f.proxyBad[i] = time.Time{}
		}
	}
	anyBad := false
	for _, until := range f.proxyBad {
		if now.Before(until) {
			anyBad = true
			break
		}
	}
	if !anyBad {
		f.proxyError = ""
	}
	f.proxyMu.Unlock()

	f.alert.Resolved("proxy", "Pobieranie przez proxy znów działa ("+used.Host+").")
	f.alert.Resolved("proxy:"+used.Host, "Proxy "+used.Host+" znów działa.")
}

func (f *Fetcher) ProxyHost() string {
	f.proxyMu.Lock()
	defer f.proxyMu.Unlock()
	if len(f.proxies) == 0 {
		return ""
	}
	now := time.Now()
	hosts := make([]string, len(f.proxies))
	for i, p := range f.proxies {
		state := ""
		switch {
		case now.Before(f.proxyBad[i]):
			state = " (pauza do " + f.proxyBad[i].Format("15:04") + ")"
		case i == f.active:
			state = " (aktywne)"
		}
		hosts[i] = p.Host + state
	}
	return strings.Join(hosts, ", ")
}

func isTimeout(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}
