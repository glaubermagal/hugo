// Copyright 2019 The Hugo Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hugolib

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/gohugoio/hugo/source"

	"github.com/gohugoio/hugo/hugofs"

	"github.com/gohugoio/hugo/common/loggers"
	"github.com/spf13/afero"
)

type pagesCollector struct {
	sourceSpec *source.SourceSpec
	fs         afero.Fs
	logger     *loggers.Logger
}

func newPagesCollector(fs afero.Fs, sp *source.SourceSpec, logger *loggers.Logger) *pagesCollector {
	return &pagesCollector{fs: fs, sourceSpec: sp, logger: logger} // TODO(bep) mo
}

const contentClassifierMetaKey = "contentc"

const (
	contentClassifierLeaf    = "branch"
	contentClassifierBranch  = "leaf"
	contentClassifierFile    = "zfile"
	contentClassifierContent = "zcontent"
)

func (c *pagesCollector) collect() error {
	preHook := func(dir hugofs.FileMetaInfo, path string, readdir []hugofs.FileMetaInfo) error {
		/* 1. If leaf bundle; start a new watcher from that path with no hooks (with the first dir preset)
		2. If branch bundle; put readdir into a bundle and continue.
		3. If assets only; send them to copy
		4. If single content files; send top handle singles + copy
		For bundles, create new FileMetaInfo for languages as needed.
		*/

		var (
			isBranchBundle bool
			isLeafBundle   bool
		)

		setClassifier := func(fi hugofs.FileMetaInfo, classifier string) {
			fi.Meta()[contentClassifierMetaKey] = classifier
		}

		for _, fi := range readdir {
			tp, isContent := classifyBundledFile(fi.Name())

			switch tp {
			case bundleLeaf:
				setClassifier(fi, contentClassifierLeaf)
				isLeafBundle = true
			case bundleBranch:
				setClassifier(fi, contentClassifierBranch)
				isBranchBundle = true
				break
			case bundleNot:
				classifier := contentClassifierFile
				if isContent {
					classifier = contentClassifierContent
				}
				setClassifier(fi, classifier)
			}
		}

		if isLeafBundle && isBranchBundle {
			// TODO(bep) mod log warning
		}

		if isLeafBundle {
			if err := c.walkLeafBundle(dir, path, readdir); err != nil {
				return err
			}
			return filepath.SkipDir
		}

		if isBranchBundle {
			fmt.Println("Handle branch bundle ...")
			return nil

		}

		if err := c.handleFiles(readdir); err != nil {
			return nil
		}

		return nil
	}

	wfn := func(info hugofs.FileMetaInfo, err error) error {
		if err != nil {
			return err
		}

		return nil
	}

	w := hugofs.NewWalkway(hugofs.WalkwayConfig{
		Fs:      c.fs,
		HookPre: preHook,
		WalkFn:  wfn})

	return w.Walk()
}

// handleFiles will receive a list of either single content files or
// static files, classified in their metadata.
func (c *pagesCollector) handleFiles(fis []hugofs.FileMetaInfo) error {
	for _, fi := range fis {
		meta := fi.Meta()
		classifier := meta.GetString(contentClassifierMetaKey)
		switch classifier {
		case contentClassifierContent:
			fmt.Println("Handle single content file ...", meta.Filename())
		case contentClassifierFile:
			fmt.Println("Handle single file ...", meta.Filename())
		default:
			panic(fmt.Sprintf("invalid classifier: %q", classifier))
		}
	}
	return nil
}

// Sort a bundle dir so the index files come first.
func (c *pagesCollector) sortBundleDir(fis []hugofs.FileMetaInfo) {
	sort.Slice(fis, func(i, j int) bool {
		fii, fij := fis[i], fis[j]
		fim, fjm := fii.Meta(), fij.Meta()

		ic, jc := fim.GetString(contentClassifierMetaKey), fjm.GetString(contentClassifierMetaKey)

		if ic < jc {
			return true
		}

		return fii.Name() < fij.Name()

	})
}

func (c *pagesCollector) isBundleHeader(fi hugofs.FileMetaInfo) bool {
	class := fi.Meta().GetString(contentClassifierMetaKey)
	return class == "leaf" || class == "branch"
}

func (c *pagesCollector) getLang(fi hugofs.FileMetaInfo) string {
	lang := fi.Meta().Lang()
	if lang != "" {
		return lang
	}

	return c.sourceSpec.DefaultContentLanguage
}

type fileinfoBundle struct {
	header    hugofs.FileMetaInfo
	resources []hugofs.FileMetaInfo
}

func (c *pagesCollector) bundleToPage(b *fileinfoBundle) *pageState {
	return nil
}

func (c *pagesCollector) cloneFileInfo(fi hugofs.FileMetaInfo) hugofs.FileMetaInfo {
	cm := hugofs.FileMeta{}
	meta := fi.Meta()
	if meta == nil {
		panic(fmt.Sprintf("not meta: %v", fi.Name()))
	}
	for k, v := range meta {
		cm[k] = v
	}

	return hugofs.NewFileMetaInfo(fi, cm)
}

func (c *pagesCollector) walkLeafBundle(dir hugofs.FileMetaInfo, path string, readdir []hugofs.FileMetaInfo) error {

	c.sortBundleDir(readdir)

	// Maps bundles to its language.
	bundles := make(map[string]*fileinfoBundle)

	getBundle := func(lang string) *fileinfoBundle {
		return bundles[lang]
	}

	cloneBundle := func(lang string) *fileinfoBundle {
		// Every bundled file needs a content file header.
		// Use the default content language if found, else just
		// pick one.
		var (
			source *fileinfoBundle
			found  bool
		)

		source, found = bundles[c.sourceSpec.DefaultContentLanguage]
		if !found {
			for _, b := range bundles {
				source = b
				break
			}
		}

		fmt.Println(">>>", len(bundles))

		clone := c.cloneFileInfo(source.header)
		clone.Meta()["lang"] = lang

		return &fileinfoBundle{
			header: clone,
		}

	}

	walk := func(info hugofs.FileMetaInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		fmt.Println(">>>", info.Name())

		lang := c.getLang(info)
		bundle := getBundle(lang)
		if bundle == nil {
			if c.isBundleHeader(info) {
				bundle = &fileinfoBundle{header: info}
				bundles[lang] = bundle
			} else {
				bundle = cloneBundle(lang)
				bundles[lang] = bundle

			}
		}

		return nil
	}

	// Start a new walker from the given path.
	w := hugofs.NewWalkway(hugofs.WalkwayConfig{
		Root:       path,
		Fs:         c.fs,
		Info:       dir,
		DirEntries: readdir,
		WalkFn:     walk})

	if err := w.Walk(); err != nil {
		return err
	}

	fmt.Printf("::: BUNDLES: %#v\n", bundles)

	return nil

}
