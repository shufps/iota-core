package storage

import (
	"sync"

	hivedb "github.com/iotaledger/hive.go/kvstore/database"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/iota-core/pkg/model"
	"github.com/iotaledger/iota-core/pkg/storage/database"
	"github.com/iotaledger/iota-core/pkg/storage/permanent"
	"github.com/iotaledger/iota-core/pkg/storage/prunable"
	"github.com/iotaledger/iota-core/pkg/storage/utils"
	iotago "github.com/iotaledger/iota.go/v4"
)

const (
	permanentDirName = "permanent"
	prunableDirName  = "prunable"

	storePrefixHealth byte = 255
)

// Storage is an abstraction around the storage layer of the node.
type Storage struct {
	dir *utils.Directory

	// Permanent is the section of the storage that is maintained forever (holds the current ledger state).
	permanent *permanent.Permanent

	// Prunable is the section of the storage that is pruned regularly (holds the history of the ledger state).
	prunable *prunable.Prunable

	shutdownOnce sync.Once
	errorHandler func(error)

	isPruning       bool
	statusLock      sync.RWMutex
	pruningLock     sync.RWMutex
	lastPrunedEpoch *model.EvictionIndex[iotago.EpochIndex]

	optsDBEngine                             hivedb.Engine
	optsAllowedDBEngines                     []hivedb.Engine
	optsPruningDelay                         iotago.EpochIndex
	optPruningSizeEnabled                    bool
	optsPruningSizeMaxTargetSizeBytes        int64
	optsPruningSizeStartThresholdPercentage  float64
	optsPruningSizeTargetThresholdPercentage float64
	optsPrunableManagerOptions               []options.Option[prunable.PrunableSlotManager]
}

// New creates a new storage instance with the named database version in the given directory.
func New(directory string, dbVersion byte, errorHandler func(error), opts ...options.Option[Storage]) *Storage {
	return options.Apply(&Storage{
		dir:                                      utils.NewDirectory(directory, true),
		errorHandler:                             errorHandler,
		lastPrunedEpoch:                          model.NewEvictionIndex[iotago.EpochIndex](),
		optsDBEngine:                             hivedb.EngineRocksDB,
		optsPruningDelay:                         30,      // TODO: what's the default now?
		optsPruningSizeMaxTargetSizeBytes:        1 << 30, // 1GB, TODO: what's the default?
		optsPruningSizeStartThresholdPercentage:  0.9,     // TODO: what's the default?
		optsPruningSizeTargetThresholdPercentage: 0.7,     // TODO: what's the default?
	}, opts,
		func(s *Storage) {
			dbConfig := database.Config{
				Engine:       s.optsDBEngine,
				Directory:    s.dir.PathWithCreate(permanentDirName),
				Version:      dbVersion,
				PrefixHealth: []byte{storePrefixHealth},
			}

			s.permanent = permanent.New(dbConfig, errorHandler)
			s.prunable = prunable.New(dbConfig.WithDirectory(s.dir.PathWithCreate(prunableDirName)), s.Settings().APIProvider(), errorHandler, s.optsPrunableManagerOptions...)
		})
}

func (s *Storage) Directory() string {
	return s.dir.Path()
}

// PrunableDatabaseSize returns the size of the underlying prunable databases.
func (s *Storage) PrunableDatabaseSize() int64 {
	return s.prunable.Size()
}

// PermanentDatabaseSize returns the size of the underlying permanent database and files.
func (s *Storage) PermanentDatabaseSize() int64 {
	return s.permanent.Size()
}

func (s *Storage) Size() int64 {
	return s.PermanentDatabaseSize() + s.PrunableDatabaseSize()
}

// Shutdown shuts down the storage.
func (s *Storage) Shutdown() {
	s.shutdownOnce.Do(func() {
		s.permanent.Shutdown()
		s.prunable.Shutdown()
	})
}

func (s *Storage) Flush() {
	s.permanent.Flush()
	s.prunable.Flush()
}
