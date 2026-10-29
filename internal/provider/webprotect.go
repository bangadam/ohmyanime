package provider

import (
	"context"
	_ "embed"
	"encoding/base64"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// webprotectionOracle signs MangaFire-family request paths with the site's
// obfuscated dynamicEncrypt routine. The algorithm lives in a VM-obfuscated
// polyfill chunk, so the chunk itself runs inside goja with browser shims;
// the per-build __config token is parsed from the challenge shell at boot.
//
// Verified against live traffic: dynamicEncrypt(plain) reproduces the site's
// vrf tokens byte for byte ("A-4gPwk" for "hello"; full search tokens match).
type webprotectionOracle struct {
	s *Service

	home   string // site origin, e.g. https://mangafire.to
	domain string // host without scheme

	mu        sync.Mutex
	vm        *goja.Runtime
	fetched   time.Time
	loadError error
}

var webprotectPolyfill = regexp.MustCompile(`(?:href|src)="(https?://[^"]*polyfill[^"]*\.js)"`)
var webprotectConfig = regexp.MustCompile(`window\.__config\s*=\s*"([^"]+)"`)
var webprotectBuild = regexp.MustCompile(`window\.__build\s*=\s*"([^"]+)"`)

// webprotectTTL bounds how long one polyfill build is trusted. The site
// rotates builds; a stale boot re-fetches on the next sign after this window.
const webprotectTTL = 12 * time.Hour

func newOracle(s *Service, home string) *webprotectionOracle {
	return &webprotectionOracle{s: s, home: home, domain: strings.TrimPrefix(strings.TrimPrefix(home, "https://"), "http://")}
}

// Sign returns the vrf token for one canonical path+query plaintext.
func (o *webprotectionOracle) Sign(ctx context.Context, plain string) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.loadError != nil && time.Since(o.fetched) < time.Minute {
		return "", o.loadError
	}
	if o.vm != nil && time.Since(o.fetched) < webprotectTTL {
		return o.call(plain)
	}
	if e := o.load(ctx); e != nil {
		return "", e
	}
	return o.call(plain)
}

func (o *webprotectionOracle) call(plain string) (string, error) {
	v, e := o.vm.RunString("globalThis.dynamicEncrypt(" + jsString(plain) + ")")
	if e != nil {
		return "", failure("malformed_source", o.domain)
	}
	out, ok := v.Export().(string)
	if !ok || out == "" {
		return "", failure("malformed_source", o.domain)
	}
	return out, nil
}

// webprotectPolyfillSrc is the site's protection polyfill, transpiled to
// ES2017 (goja rejects async generators) and bundled IIFE. The polyfill's
// algorithm is build-stable; the per-deployment secret arrives in the
// __config token fetched from the shell at boot.
//
//go:embed webprotect_polyfill.js
var webprotectPolyfillSrc string

// load fetches the challenge shell for the live __config token, then boots
// the goja VM with the embedded polyfill.
func (o *webprotectionOracle) load(ctx context.Context) error {
	o.fetched = time.Now()
	shell, e := o.s.get(ctx, o.home+"/", o.home+"/")
	if e != nil {
		return e
	}
	cm := webprotectConfig.FindSubmatch(shell)
	bm := webprotectBuild.FindSubmatch(shell)
	if len(cm) != 2 {
		return failure("malformed_source", o.domain)
	}
	build := ""
	if len(bm) == 2 {
		build = string(bm[1])
	}
	vm := goja.New()
	if _, e = vm.RunString(webprotectShim(o.domain, string(cm[1]), build)); e != nil {
		return failure("malformed_source", o.domain)
	}
	if _, e = vm.RunString(webprotectPolyfillSrc); e != nil {
		return failure("malformed_source", o.domain)
	}
	if vm.Get("dynamicEncrypt") == nil {
		return failure("malformed_source", o.domain)
	}
	o.vm = vm
	o.loadError = nil
	return nil
}

// jsString quotes a Go string as a JS string literal via atob of its base64,
// so no escaping edge case can produce invalid JavaScript.
func jsString(s string) string {
	return "atob(" + strconv.Quote(base64.StdEncoding.EncodeToString([]byte(s))) + ")"
}

// webprotectShim provides the browser globals the polyfill probes. The set
// was bisected against the live chunk; removing any piece breaks its boot.
func webprotectShim(domain, config, build string) string {
	parts := []string{
		"globalThis.b64tab = \"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/\";",
		"globalThis.window = globalThis;",
		"globalThis.atob = (s) => { s = s.replace(/=+$/, \"\"); let o = \"\"; for (let i = 0; i < s.length; i += 4) { const g = (k) => (k < s.length) ? globalThis.b64tab.indexOf(s[k]) : -1; const n = [g(i), g(i+1), g(i+2), g(i+3)]; o += String.fromCharCode((n[0]<<2)|((n[1]>>4)||0)); if (n[2] >= 0) o += String.fromCharCode(((n[1]&15)<<4)|(n[2]>>2)); if (n[3] >= 0) o += String.fromCharCode(((n[2]&3)<<6)|n[3]); } return o; };",
		"globalThis.__config = " + jsString(config) + ";",
		"globalThis.__build = " + jsString(build) + ";",
		"globalThis.config = {_DEBUG_MODE:\"0\",_VALID_DOMAIN:\"1\",_TRACKING_ID:\"2\",_CAPTCHA_KEY:\"3\",_GEO_TIER:\"4\",_data:[0," + jsString(domain) + ",\"af266caa520a\",\"0x4AAAAAAA9HVepYlLLsGrEj\",\"normal\"]};",
		"globalThis.location = {href:\"https://" + domain + "/\",origin:\"https://" + domain + "\",protocol:\"https:\",host:" + jsString(domain) + ",hostname:" + jsString(domain) + ",port:\"\",pathname:\"/\",search:\"\",hash:\"\",assign(){},replace(){},reload(){}};",
		"globalThis.document = {createElement:()=>({style:{},setAttribute(){},getAttribute(){return null},appendChild(){},addEventListener(){},classList:{add(){},remove(){},contains(){return false}},textContent:\"\",innerHTML:\"\",tagName:\"div\",children:[],firstChild:null,parentNode:null,sheet:{cssRules:[],insertRule(){}},getContext:()=>null}),createTextNode:()=>({}),documentElement:{style:{},appendChild(){}},head:{appendChild(){}},body:{appendChild(){}},addEventListener(){},removeEventListener(){},cookie:\"\",readyState:\"complete\",currentScript:null,domain:" + jsString(domain) + ",referrer:\"\",title:\"\",getElementsByTagName:()=>[],querySelector:()=>null,querySelectorAll:()=>[]};",
		"globalThis.addEventListener = () => {}; globalThis.removeEventListener = () => {}; globalThis.dispatchEvent = () => true;",
		"globalThis.matchMedia = () => ({matches:false,addListener(){},removeListener(){},addEventListener(){},removeEventListener(){}});",
		"globalThis.performance = {now:()=>Date.now(),timing:{navigationStart:Date.now()},getEntriesByType:()=>[],getEntriesByName:()=>[]};",
		"globalThis.crypto = {getRandomValues:(a)=>{for(let i=0;i<a.length;i++)a[i]=(Math.random()*256)|0;return a;}};",
		"globalThis.devicePixelRatio = 1; globalThis.innerWidth = 1280; globalThis.innerHeight = 720;",
		"globalThis.MutationObserver = class {observe(){} disconnect(){} takeRecords(){return []}};",
		"globalThis.setTimeout = () => 0; globalThis.clearTimeout = () => {}; globalThis.setInterval = () => 0; globalThis.clearInterval = () => {}; globalThis.requestAnimationFrame = () => 0; globalThis.cancelAnimationFrame = () => {};",
		"globalThis.getComputedStyle = () => ({getPropertyValue:()=>\"\"});",
		"globalThis.alert = () => {}; globalThis.confirm = () => true; globalThis.prompt = () => \"\";",
		"globalThis.name = \"\";",
		"globalThis.navigator = {userAgent:\"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/156.0.0.0 Safari/537.36\",appCodeName:\"Mozilla\",appName:\"Netscape\",appVersion:\"5.0\",language:\"en-US\",languages:[\"en-US\",\"en\"],platform:\"MacIntel\",product:\"Gecko\",productSub:\"20030107\",vendor:\"Google Inc.\",vendorSub:\"\",hardwareConcurrency:8,maxTouchPoints:0,webdriver:false,cookieEnabled:true,onLine:true,javaEnabled:()=>false,sendBeacon:()=>true,plugins:[],mimeTypes:[]};",
		"globalThis.screen = {width:1280,height:720,availWidth:1280,availHeight:720,colorDepth:24,pixelDepth:24};",
		"globalThis.b64tab = \"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/\";",
		"globalThis.btoa = (s) => { let o = \"\"; for (let i = 0; i < s.length; i += 3) { const a = [s.charCodeAt(i), s.charCodeAt(i+1), s.charCodeAt(i+2)]; o += globalThis.b64tab[a[0]>>2]; o += globalThis.b64tab[((a[0]&3)<<4)|(isNaN(a[1])?0:a[1]>>4)]; o += isNaN(a[1])?\"=\":globalThis.b64tab[((a[1]&15)<<2)|(isNaN(a[2])?0:a[2]>>6)]; o += isNaN(a[2])?\"=\":globalThis.b64tab[a[2]&63]; } return o; };",
		"globalThis.atob = (s) => { s = s.replace(/=+$/, \"\"); let o = \"\"; for (let i = 0; i < s.length; i += 4) { const g = (k) => (k < s.length) ? globalThis.b64tab.indexOf(s[k]) : -1; const n = [g(i), g(i+1), g(i+2), g(i+3)]; o += String.fromCharCode((n[0]<<2)|((n[1]>>4)||0)); if (n[2] >= 0) o += String.fromCharCode(((n[1]&15)<<4)|(n[2]>>2)); if (n[3] >= 0) o += String.fromCharCode(((n[2]&3)<<6)|n[3]); } return o; };",
		"globalThis.TextEncoder = class { encode(s) { const b = []; for (let i = 0; i < s.length; i++) { let c = s.codePointAt(i); if (c > 0xffff) i++; if (c < 0x80) b.push(c); else if (c < 0x800) { b.push(0xc0|(c>>6), 0x80|(c&63)); } else if (c < 0x10000) { b.push(0xe0|(c>>12), 0x80|((c>>6)&63), 0x80|(c&63)); } else { b.push(0xf0|(c>>18), 0x80|((c>>12)&63), 0x80|((c>>6)&63), 0x80|(c&63)); } } return new Uint8Array(b); } };",
		"globalThis.TextDecoder = class { decode(b) { let s = \"\"; for (let i = 0; i < b.length; ) { const c = b[i]; if (c < 0x80) { s += String.fromCharCode(c); i++; } else if (c < 0xe0) { s += String.fromCharCode(((c&31)<<6)|(b[i+1]&63)); i += 2; } else if (c < 0xf0) { s += String.fromCharCode(((c&15)<<12)|((b[i+1]&63)<<6)|(b[i+2]&63)); i += 3; } else { s += String.fromCodePoint(((c&7)<<18)|((b[i+1]&63)<<12)|((b[i+2]&63)<<6)|(b[i+3]&63)); i += 4; } } return s; } };",
		"const __st = () => { const m = new Map(); return {getItem:(k)=>m.has(k)?m.get(k):null,setItem:(k,v)=>m.set(k,String(v)),removeItem:(k)=>m.delete(k),clear:()=>m.clear(),key:(i)=>[...m.keys()][i]??null,get length(){return m.size}}; };",
		"globalThis.localStorage = __st(); globalThis.sessionStorage = __st();",
		"globalThis.XMLHttpRequest = class { open(){} send(){} setRequestHeader(){} addEventListener(){} abort(){} };",
		"globalThis.fetch = () => Promise.resolve({ok:true,json:()=>Promise.resolve({}),text:()=>Promise.resolve(\"\")});",
		"globalThis.self = globalThis; globalThis.top = globalThis; globalThis.parent = globalThis;",
	}
	return strings.Join(parts, "\n")
}

// canonicalQuery renders params the way the site's axios interceptor signs
// them: keys sorted alphabetically by rendered name (arrays already carry
// their [index] suffixes), values raw — never percent-encoded.
func canonicalQuery(path string, params [][2]string) string {
	if len(params) == 0 {
		return path
	}
	rendered := make([]string, 0, len(params))
	for _, kv := range params {
		rendered = append(rendered, kv[0]+"="+kv[1])
	}
	sort.Strings(rendered)
	return path + "?" + strings.Join(rendered, "&")
}
