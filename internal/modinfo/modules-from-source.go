package modinfo

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/sirkon/goproxy/semver"
)

// NewChannelFromSource run over file with 'module@version' or just 'module' content at each line (empty lines are ignored)
func NewChannelFromSource(fileName string, src io.Reader) chan Result {
	res := make(chan Result)
	go func() {
		defer close(res)

		scanner := bufio.NewScanner(src)
		var lineNo int
		for scanner.Scan() {
			if len(scanner.Text()) == 0 {
				lineNo++
				continue
			}
			mod, ver, err := modVersion(scanner.Text())
			if err != nil {
				res <- Error{
					errMsg: fmt.Sprintf("%s:%d %s", fileName, lineNo, err),
				}
				return
			}
			if len(ver) > 0 {
				res <- Module{
					Path:    mod,
					Version: ver,
				}
			} else {
				res <- Latest{
					Path: mod,
				}
			}
			lineNo++
		}
		if err := scanner.Err(); err != nil {
			res <- Error{
				errMsg: fmt.Sprintf("error scanning  `%s`: %s", fileName, err),
			}
			return
		}
	}()
	return res
}

func modVersion(line string) (string, string, error) {
	pos := strings.IndexByte(line, '@')
	if pos < 0 {
		return line, "", nil
	}
	versionLit := line[pos+1:]
	if !semver.IsValid(versionLit) {
		return "", "", fmt.Errorf("invalid semver %s", versionLit)
	}
	return line[:pos], versionLit, nil
}
