// Copyright 2016 Jacques Supcik, Blue Masters
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Bitmap fonts

package font

import (
	"bytes"
)

var alias = map[rune]string{
	0x2764:     "\u2665", // ❤
	0x0001f499: "\u2665", // 💙
	0x0001f49a: "\u2665", // 💚
	0x0001f49b: "\u2665", // 💛
	0x0001f49c: "\u2665", // 💜
	0x0001f49d: "\u2665", // 💝
	0x0001F601: ":|",     // 😁
	0x0001F602: ":)",     // 😂
	0x0001F603: ":D",     // 😃
}

// Font is the base type for fonts
type Font struct {
	Width  int             `json:"width"`
	Height int             `json:"height"`
	Bitmap map[rune][]byte `json:"bitmap"`
}

// ExpandAlias replaces special characters (such as emoticons)
// by printable strings
func ExpandAlias(text string) (string, error) {
	var f func(b *bytes.Buffer, s string) error
	f = func(b *bytes.Buffer, s string) error {
		for _, c := range s {
			m, ok := alias[c]
			if ok {
				if err := f(b, m); err != nil {
					return err
				}
			} else {
				if _, err := b.WriteRune(c); err != nil {
					return err
				}
			}
		}
		return nil
	}
	b := new(bytes.Buffer)
	err := f(b, text)
	return b.String(), err
}
