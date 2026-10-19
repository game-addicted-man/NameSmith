// Command winres writes the Windows resources of NameSmith (icon, manifest,
// version information) into a .syso file that `go build` links in.
//
//	cd build/winres && go run . -version 1.0.0
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func main() {
	ver := flag.String("version", "1.0.0", "program version (x.y.z)")
	icon := flag.String("icon", "../icon.ico", "icon file")
	out := flag.String("out", "../../cmd/NameSmith/rsrc_windows_%s.syso", "output path (%s = arch)")
	arches := flag.String("arch", "amd64,arm64", "comma-separated architectures")
	flag.Parse()

	rs := &winres.ResourceSet{}

	f, err := os.Open(*icon)
	if err != nil {
		log.Fatal(err)
	}
	ico, err := winres.LoadICO(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}
	if err := rs.SetIcon(winres.ID(1), ico); err != nil {
		log.Fatal(err)
	}

	rs.SetManifest(winres.AppManifest{
		Description:         "NameSmith - 파일 이름 일괄 변경기",
		Compatibility:       winres.Win7AndAbove,
		ExecutionLevel:      winres.AsInvoker,
		DPIAwareness:        winres.DPIPerMonitorV2,
		LongPathAware:       true,
		UseCommonControlsV6: true,
	})

	var vi version.Info
	vi.SetFileVersion(*ver)
	vi.SetProductVersion(*ver)
	const ko = 0x0412 // Korean
	for _, lang := range []uint16{version.LangDefault, ko} {
		must(vi.Set(lang, version.FileDescription, "NameSmith - 파일 이름 일괄 변경기"))
		must(vi.Set(lang, version.ProductName, "NameSmith"))
		must(vi.Set(lang, version.CompanyName, "game-addicted-man"))
		must(vi.Set(lang, version.LegalCopyright, "© 2026 Jonghoon Lee · MIT License"))
		must(vi.Set(lang, version.OriginalFilename, "NameSmith.exe"))
		must(vi.Set(lang, version.InternalName, "NameSmith"))
		must(vi.Set(lang, version.FileVersion, *ver))
		must(vi.Set(lang, version.ProductVersion, *ver))
		must(vi.Set(lang, version.Comments, "https://github.com/game-addicted-man/NameSmith"))
	}
	rs.SetVersionInfo(vi)

	for _, a := range strings.Split(*arches, ",") {
		path := *out
		if strings.Contains(path, "%s") {
			path = fmt.Sprintf(path, a)
		}
		w, err := os.Create(path)
		if err != nil {
			log.Fatal(err)
		}
		if err := rs.WriteObject(w, winres.Arch(a)); err != nil {
			log.Fatal(err)
		}
		w.Close()
		fmt.Println("wrote", path)
	}
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
