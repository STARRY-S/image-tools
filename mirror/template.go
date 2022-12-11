package mirror

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cnrancher.io/image-tools/image"
	u "cnrancher.io/image-tools/utils"
	"github.com/sirupsen/logrus"
	"golang.org/x/exp/slices"
)

type SavedListTemplate struct {
	List      []SavedMirrorTemplate
	Version   string
	SavedTime string
}

type SavedMirrorTemplate struct {
	Source   string
	Tag      string
	ArchList []string
	Images   []SavedImagesTemplate
}

type SavedImagesTemplate struct {
	Arch    string
	OS      string
	Variant string
	Folder  string
}

func NewSavedListTemplate() *SavedListTemplate {
	return &SavedListTemplate{
		List:      make([]SavedMirrorTemplate, 0),
		Version:   u.VERSION,
		SavedTime: time.Now().Format(time.RFC3339),
	}
}

func (s *SavedListTemplate) Append(mT *SavedMirrorTemplate) {
	if mT == nil {
		return
	}
	s.List = append(s.List, *mT)
}

func (m *Mirror) GetSavedImageTemplate() *SavedMirrorTemplate {
	if m.Mode != MODE_SAVE {
		return nil
	}

	mT := SavedMirrorTemplate{
		Source:   m.Source,
		Tag:      m.Tag,
		ArchList: make([]string, 0),
		Images:   make([]SavedImagesTemplate, 0),
	}
	for _, img := range m.images {
		if mT.ArchList == nil ||
			!slices.Contains(mT.ArchList, img.Arch) {
			mT.ArchList = append(mT.ArchList, img.Arch)
		}
		iT := SavedImagesTemplate{
			Arch:    img.Arch,
			OS:      img.OS,
			Variant: img.Variant,
			Folder:  img.SavedFolder,
		}
		mT.Images = append(mT.Images, iT)
	}
	if len(mT.ArchList) == 0 {
		return nil
	}

	return &mT
}

// LoadSavedTemplates loads the saved json templates to *Mirror slice
func LoadSavedTemplates(directory, destReg string) ([]*Mirror, error) {
	var err error
	if directory, err = u.GetAbsPath(directory); err != nil {
		return nil, fmt.Errorf("LoadSavedMirrorTemplate: %w", err)
	}
	logrus.Debugf("LoadSavedTemplates from dir: %v", directory)

	savedList := SavedListTemplate{}
	f, err := os.Open(filepath.Join(directory, u.SavedImageListFile))
	if err != nil {
		return nil, fmt.Errorf("LoadSavedMirrorTemplate: %w", err)
	}
	err = json.NewDecoder(f).Decode(&savedList)
	if err != nil {
		return nil, fmt.Errorf("LoadSavedMirrorTemplate: %w", err)
	}

	// TODO: Compare version

	var mirrorList []*Mirror
	for i, mT := range savedList.List {
		m := NewMirror(&MirrorOptions{
			Source:      mT.Source,
			Destination: u.ConstructRegistry(mT.Source, destReg),
			Directory:   directory,
			Tag:         mT.Tag,
			ArchList:    mT.ArchList,
			Mode:        MODE_LOAD,
			ID:          i + 1,
		})

		for _, iT := range mT.Images {
			copiedTag := image.CopiedTag(mT.Tag, iT.OS, iT.Arch, iT.Variant)
			// Source is a directory
			srcImageDir := filepath.Join(directory, iT.Folder)
			// Destination is the dest registry
			repo := u.ConstructRegistry(mT.Source, destReg)
			destImage := fmt.Sprintf("%s:%s", repo, copiedTag)
			img := image.NewImage(&image.ImageOptions{
				Source:      srcImageDir,
				Destination: destImage,
				// Directory is the decompressed folder path
				Directory:   directory,
				Tag:         mT.Tag,
				Arch:        iT.Arch,
				Variant:     iT.Variant,
				OS:          iT.OS,
				SavedFolder: iT.Folder,

				// saved image manifest is already converted to v2s2
				SourceSchemaVersion: 2,
				SourceMediaType:     u.MediaTypeManifestV2,
			})
			m.AppendImage(img)
		}
		mirrorList = append(mirrorList, m)
	}

	return mirrorList, nil
}
