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

// Package hugofs provides the file systems used by Hugo.
package hugofs

import (
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/spf13/cast"

	"github.com/gohugoio/hugo/common/hreflect"

	"github.com/spf13/afero"
)

const (
	metaKeyFilename  = "filename"
	metaKeyName      = "name"
	metaKeyPath      = "path"
	metaKeyLang      = "lang"
	metaKeyWeight    = "weight"
	metaKeyFs        = "fs"
	metaKeyOpener    = "opener"
	metaKeyIsOrdered = "isOrdered"
	metaKeyIsSymlink = "isSymlink"
)

type FileMeta map[string]interface{}

func (f FileMeta) GetInt(key string) int {
	return cast.ToInt(f[key])
}

func (f FileMeta) GetString(key string) string {
	return cast.ToString(f[key])
}

func (f FileMeta) GetBool(key string) bool {
	return cast.ToBool(f[key])
}

func (f FileMeta) Filename() string {
	return f.stringV(metaKeyFilename)
}

func (f FileMeta) Name() string {
	return f.stringV(metaKeyName)
}

func (f FileMeta) Lang() string {
	return f.stringV(metaKeyLang)
}

func (f FileMeta) Path() string {
	return f.stringV(metaKeyPath)
}

func (f FileMeta) Weight() int {
	return f.GetInt(metaKeyWeight)
}

func (f FileMeta) IsOrdered() bool {
	return f.GetBool(metaKeyIsOrdered)
}

// IsSymlink returns whether this comes from a symlinked file or directory.
func (f FileMeta) IsSymlink() bool {
	return f.GetBool(metaKeyIsSymlink)
}

func (f FileMeta) Watch() bool {
	if v, found := f["watch"]; found {
		return v.(bool)
	}
	return false
}

func (f FileMeta) Fs() afero.Fs {
	if v, found := f[metaKeyFs]; found {
		return v.(afero.Fs)
	}
	return nil
}

func (f FileMeta) GetOpener() func() (afero.File, error) {
	o, found := f[metaKeyOpener]
	if !found {
		return nil
	}
	return o.(func() (afero.File, error))
}

func (f FileMeta) Open() (afero.File, error) {
	v, found := f[metaKeyOpener]
	if !found {
		return nil, errors.New("file opener not found")
	}
	return v.(func() (afero.File, error))()
}

func (f FileMeta) stringV(key string) string {
	if v, found := f[key]; found {
		return v.(string)
	}
	return ""
}

func (f FileMeta) setIfNotZero(key string, val interface{}) {
	if !hreflect.IsTruthful(val) {
		return
	}
	f[key] = val
}

type FileMetaInfo interface {
	os.FileInfo
	Meta() FileMeta
}

type fileInfoMeta struct {
	os.FileInfo
	m FileMeta
}

func (fi *fileInfoMeta) Meta() FileMeta {
	return fi.m
}

func NewFileMetaInfo(fi os.FileInfo, m FileMeta) FileMetaInfo {
	if fim, ok := fi.(FileMetaInfo); ok {
		mergeFileMeta(fim.Meta(), m)
	}
	return &fileInfoMeta{FileInfo: fi, m: m}
}

// Merge metadata, last entry wins.
func mergeFileMeta(from, to FileMeta) {
	if from == nil {
		return
	}
	for k, v := range from {
		if _, found := to[k]; !found {
			to[k] = v
		}
	}
}

type dirNameOnlyFileInfo struct {
	name string
}

func (fi *dirNameOnlyFileInfo) Name() string {
	return fi.name
}

func (fi *dirNameOnlyFileInfo) Size() int64 {
	panic("not implemented")
}

func (fi *dirNameOnlyFileInfo) Mode() os.FileMode {
	return os.ModeDir
}

func (fi *dirNameOnlyFileInfo) ModTime() time.Time {
	panic("not implemented")
}

func (fi *dirNameOnlyFileInfo) IsDir() bool {
	return true
}

func (fi *dirNameOnlyFileInfo) Sys() interface{} {
	return nil
}

func newDirNameOnlyFileInfo(name string, isOrdered bool, fileOpener func() (afero.File, error)) FileMetaInfo {
	return NewFileMetaInfo(&dirNameOnlyFileInfo{name: name}, FileMeta{
		metaKeyFilename:  name,
		metaKeyIsOrdered: isOrdered,
		metaKeyOpener:    fileOpener})
}

func decorateFileInfo(
	decid string,
	fi os.FileInfo,
	fs afero.Fs, opener func() (afero.File, error),
	filename, filepath string, inMeta FileMeta) os.FileInfo {

	var meta FileMeta

	filepath = strings.TrimPrefix(filepath, string(os.PathSeparator))

	if fim, ok := fi.(FileMetaInfo); ok {
		meta = fim.Meta()
		if fn, ok := meta[metaKeyFilename]; ok {
			filename = fn.(string)
		}
	} else {
		meta = make(FileMeta)
		fim := NewFileMetaInfo(fi, meta)
		fi = fim

	}

	// For debugging
	if idv, ok := meta["decoraterPath"]; ok {
		meta["decoraterPath"] = path.Join(idv.(string), decid)
	} else {
		meta["decoraterPath"] = decid
	}

	if opener != nil {
		//meta.setIfNotZero(metaKeyOpener, opener)
		meta[metaKeyOpener] = opener

	}

	if fi.IsDir() && fs != nil { // TODO(bep) mod check + argument
		meta.setIfNotZero(metaKeyFs, fs)
	}

	meta.setIfNotZero(metaKeyPath, filepath)
	meta.setIfNotZero(metaKeyFilename, filename)

	mergeFileMeta(inMeta, meta)

	return fi

}

func isSymlink(fi os.FileInfo) bool {
	return fi != nil && fi.Mode()&os.ModeSymlink == os.ModeSymlink
}

func fileInfosToFileMetaInfos(fis []os.FileInfo) []FileMetaInfo {
	fims := make([]FileMetaInfo, len(fis))
	for i, v := range fis {
		fims[i] = v.(FileMetaInfo)
	}
	return fims
}
