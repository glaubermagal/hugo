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

package hugofs

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pkg/errors"

	"github.com/spf13/afero"
)

type (
	WalkFunc func(info FileMetaInfo, err error) error
	WalkHook func(dir FileMetaInfo, path string, readdir []FileMetaInfo) error
)

type Walkway struct {
	fs   afero.Fs
	root string

	// May be pre-set
	fi         FileMetaInfo
	dirEntries []FileMetaInfo

	walkFn WalkFunc
	walked bool

	// We may traverse symbolic links and bite ourself.
	seen map[string]bool

	// Optional hooks
	hookPre  WalkHook
	hookPost WalkHook
}

type WalkwayConfig struct {
	Fs   afero.Fs
	Root string

	// One or both of these may be pre-set.
	Info       FileMetaInfo
	DirEntries []FileMetaInfo

	WalkFn   WalkFunc
	HookPre  func(dir FileMetaInfo, path string, readdir []FileMetaInfo) error
	HookPost func(dir FileMetaInfo, path string, readdir []FileMetaInfo) error
}

func NewWalkway(cfg WalkwayConfig) *Walkway {
	var fs afero.Fs
	if cfg.Info != nil {
		fs = cfg.Info.Meta().Fs()
	} else {
		fs = cfg.Fs
	}

	return &Walkway{
		fs:         fs,
		root:       cfg.Root,
		fi:         cfg.Info,
		dirEntries: cfg.DirEntries,
		walkFn:     cfg.WalkFn,
		hookPre:    cfg.HookPre,
		hookPost:   cfg.HookPost,
		seen:       make(map[string]bool)}
}

// TODO(bep) make content use this
func (w *Walkway) Walk() error {
	if w.walked {
		panic("this walkway is already walked")
	}
	w.walked = true

	if w.fs == NoOpFs {
		return nil
	}

	var fi FileMetaInfo
	if w.fi != nil {
		fi = w.fi
	} else {
		info, err := lstatIfPossible(w.fs, w.root)
		if err != nil {
			return w.walkFn(nil, errors.Wrapf(err, "walk: %q", w.root))
		}
		fi = info.(FileMetaInfo)
	}

	if !fi.IsDir() {
		return w.walkFn(nil, errors.New("file to walk must be a directory"))
	}

	return w.walk(w.root, fi, w.dirEntries, w.walkFn)

}

// if the filesystem supports it, use Lstat, else use fs.Stat
func lstatIfPossible(fs afero.Fs, path string) (os.FileInfo, error) {
	if lfs, ok := fs.(afero.Lstater); ok {
		fi, _, err := lfs.LstatIfPossible(path)
		return fi, err
	}
	return fs.Stat(path)
}

// walk recursively descends path, calling walkFn.
// It follow symlinks if supported by the filesystem, but only the same path once.
func (w *Walkway) walk(path string, info FileMetaInfo, dirEntries []FileMetaInfo, walkFn WalkFunc) error {
	err := walkFn(info, nil)
	if err != nil {
		if info.IsDir() && err == filepath.SkipDir {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}

	meta := info.Meta()
	filename := meta.Filename()
	filenameToOpen := path // may be a composite
	openFs := w.fs

	if meta.IsSymlink() {
		// Symlinks will only work in the filesystems defined by the project,
		// (not theme components), and we do follow them.
		filenameToOpen = filename
		// This is a full filename to a file on the Os filesystem.
		openFs = osDecorated

	}

	// Prevent infinite recursion.
	w.isSeen(filename)

	f, err := openFs.Open(filenameToOpen)

	if err != nil {
		return walkFn(info, errors.Wrapf(err, "walk: open %q (path: %q)", filenameToOpen, path))
	}

	if dirEntries == nil {
		fis, err := f.Readdir(-1)
		f.Close()
		if err != nil {
			return walkFn(info, errors.Wrap(err, "walk: Readdir"))
		}

		dirEntries = fileInfosToFileMetaInfos(fis)

		if !meta.IsOrdered() {
			sort.Slice(fis, func(i, j int) bool {
				fii := dirEntries[i]
				fij := dirEntries[j]
				return fii.Name() < fij.Name()
			})
		}
	}

	if w.hookPre != nil {
		if err := w.hookPre(info, path, dirEntries); err != nil {
			if err == filepath.SkipDir {
				return nil
			}
			return err
		}
	}

	for _, fi := range dirEntries {
		fim := fi.(FileMetaInfo)
		var err error

		meta := fim.Meta()

		// Note that we use the original Name even if it's a symlink.
		pathn := filepath.Join(path, meta.Name())

		meta[metaKeyPath] = w.relativePath(pathn)

		if err != nil {
			return walkFn(fim, err)
		}

		if fim.IsDir() {

			// Prevent infinite recursion
			filename := meta.Filename()
			if w.isSeen(filename) && meta.IsSymlink() {
				// Possible cyclic reference
				// TODO(bep) mod check if we log some warning about this in the
				// existing content walker.
				continue
			}
		}

		err = w.walk(pathn, fim, nil, walkFn)
		if err != nil {
			if !fi.IsDir() || err != filepath.SkipDir {
				return err
			}
		}
	}

	if w.hookPost != nil {
		if err := w.hookPost(info, path, dirEntries); err != nil {
			if err == filepath.SkipDir {
				return nil
			}
			return err
		}
	}
	return nil
}

func (w *Walkway) isSeen(filename string) bool {
	if w.seen[filename] {
		return true
	}

	w.seen[filename] = true
	return false
}

func (w *Walkway) relativePath(path string) string {
	return strings.TrimPrefix(strings.TrimPrefix(path, w.root), filepathSeparator)
}
