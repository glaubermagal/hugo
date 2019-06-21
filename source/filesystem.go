// Copyright 2016 The Hugo Authors. All rights reserved.
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

package source

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/pkg/errors"

	"github.com/gohugoio/hugo/hugofs"

	jww "github.com/spf13/jwalterweatherman"
)

// Filesystem represents a source filesystem.
type Filesystem struct {
	files        []ReadableFile
	filesInit    sync.Once
	filesInitErr error

	Base string

	fi hugofs.FileMetaInfo

	SourceSpec
}

// Input describes a source input.
type Input interface {
	Files() []ReadableFile
}

// NewFilesystem returns a new filesytem for a given source spec.
func (sp SourceSpec) NewFilesystem(base string) *Filesystem {
	return &Filesystem{SourceSpec: sp, Base: base}
}

func (sp SourceSpec) NewFilesystemFromFileMetaInfo(fi hugofs.FileMetaInfo) *Filesystem {
	return &Filesystem{SourceSpec: sp, fi: fi}
}

// Files returns a slice of readable files.
func (f *Filesystem) Files() ([]ReadableFile, error) {
	f.filesInit.Do(func() {
		err := f.captureFiles()
		if err != nil {
			f.filesInitErr = errors.Wrap(err, "capture files")
		}
	})
	return f.files, f.filesInitErr
}

// add populates a file in the Filesystem.files
func (f *Filesystem) add(name string, fi hugofs.FileMetaInfo) (err error) {
	var file ReadableFile

	if runtime.GOOS == "darwin" {
		// When a file system is HFS+, its filepath is in NFD form.
		// TODO(bep) mod move this to hugofs name = norm.NFC.String(name)
	}

	file, err = f.SourceSpec.NewFileInfo(fi, false)
	if err != nil {
		return err
	}

	f.files = append(f.files, file)

	return err
}

func (f *Filesystem) captureFiles() error {
	walker := func(fi hugofs.FileMetaInfo, err error) error {
		if err != nil {
			return err
		}

		if fi.IsDir() {
			return nil
		}

		meta := fi.Meta()
		filename := meta.Filename()

		b, err := f.shouldRead(filename, fi)
		if err != nil {
			return err
		}

		if b {
			err = f.add(filename, fi)
		}

		return err
	}

	w := hugofs.NewWalkway(hugofs.WalkwayConfig{
		Fs:     f.SourceFs,
		Info:   f.fi,
		Root:   f.Base,
		WalkFn: walker,
	})

	return w.Walk()

}

func (f *Filesystem) shouldRead(filename string, fi hugofs.FileMetaInfo) (bool, error) {
	if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
		link, err := filepath.EvalSymlinks(filename)
		if err != nil {
			jww.ERROR.Printf("Cannot read symbolic link '%s', error was: %s", filename, err)
			return false, nil
		}
		// TODO(bep) mod this vs root mapping
		linkfi, err := f.SourceFs.Stat(link)
		if err != nil {
			jww.ERROR.Printf("Cannot stat '%s', error was: %s", link, err)
			return false, nil
		}

		if !linkfi.Mode().IsRegular() {
			jww.ERROR.Printf("Symbolic links for directories not supported, skipping '%s'", filename)
		}
		return false, nil
	}

	ignore := f.SourceSpec.IgnoreFile(fi.Meta().Filename())

	if fi.IsDir() {
		if ignore {
			return false, filepath.SkipDir
		}
		return false, nil
	}

	if ignore {
		return false, nil
	}

	return true, nil
}
