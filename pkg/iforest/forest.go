package iforest

import (
	"math"
	"math/rand"
	"sync"
)

const (
	defaultNumTrees       = 100
	defaultSampleSize     = 256
	defaultScoreThreshold = 0.6
	defaultDetectionType  = DetectionTypeThreshold
	offset                = 0.5
)

// DetectionType specifies the method used for detecting anomalies.
// Possible values:
//   - DetectionTypeThreshold: uses a fixed score threshold for anomaly detection.
//   - DetectionTypeProportion: uses a proportion of the dataset to determine the threshold.
type DetectionType string

const (
	DetectionTypeThreshold  DetectionType = "threshold"
	DetectionTypeProportion DetectionType = "proportion"
)

// Options contains configuration settings for the IsolationForest.
// Fields:
//
//	DetectionType - the method used for anomaly detection ("threshold" or "proportion").
//	Threshold     - the score threshold for classifying anomalies (used if DetectionType is "threshold").
//	Proportion    - the proportion of data points to classify as anomalies (used if DetectionType is "proportion").
//	NumTrees      - the number of trees to build in the forest.
//	SampleSize    - the number of samples to use for building each tree.
//	MaxDepth      - the maximum depth allowed for each tree.
type Options struct {
	DetectionType DetectionType `json:"detection_type"`
	Threshold     float64       `json:"threshold"`
	Proportion    float64       `json:"proportion"`
	NumTrees      int           `json:"num_trees"`
	SampleSize    int           `json:"sample_size"`
	MaxDepth      int           `json:"max_depth"`
}

// SetDefaultValues assigns default values to any unset fields in Options.
// This ensures all necessary options have sensible defaults before they are used.
func (o *Options) SetDefaultValues() {
	if o.DetectionType == "" {
		o.DetectionType = defaultDetectionType
	}

	if o.Threshold == 0 {
		o.Threshold = defaultScoreThreshold
	}

	if o.NumTrees == 0 {
		o.NumTrees = defaultNumTrees
	}

	if o.SampleSize == 0 {
		o.SampleSize = defaultSampleSize
	}

	if o.MaxDepth == 0 {
		o.MaxDepth = int(math.Ceil(math.Log2(float64(o.SampleSize))))
	}
}

// IsolationForest represents the isolation forest model used for anomaly detection.
// Fields:
//
//	Options - the configuration options for the model.
//	Trees - the collection of isolation trees built during training.
type IsolationForest struct {
	*Options

	Trees []*TreeNode
}

// New creates a new IsolationForest with default options.
// Returns:
//
//	A pointer to an initialized IsolationForest instance with default settings.
func New() *IsolationForest {
	options := &Options{}
	options.SetDefaultValues()
	return &IsolationForest{Options: options}
}

// NewWithOptions creates a new IsolationForest with the specified options.
// Parameters:
//
//	options - the Options struct specifying configuration for the model.
//
// Returns:
//
//	A pointer to an initialized IsolationForest instance configured with the provided options.
func NewWithOptions(options Options) *IsolationForest {
	options.SetDefaultValues()
	return &IsolationForest{Options: &options}
}

// Fit trains the isolation forest using the provided samples.
// Parameters:
//
//	samples - a Matrix of data points to train the model on.
//
// This method builds multiple isolation trees in parallel using the samples.
func (f *IsolationForest) Fit(samples [][]float64) {
	wg := sync.WaitGroup{}
	wg.Add(f.NumTrees)

	f.Trees = make([]*TreeNode, f.NumTrees)
	for i := 0; i < f.NumTrees; i++ {
		sampled := Sample(samples, f.SampleSize)
		go func(index int) {
			defer wg.Done()
			tree := f.BuildTree(sampled, 0)
			f.Trees[index] = tree
		}(i)
	}
	wg.Wait()
}

// BuildTree constructs an isolation tree from the samples recursively.
// Parameters:
//
//	samples - a Matrix of data points used to build the tree.
//	depth   - the current depth in the tree during recursive calls.
//
// Returns:
//
//	A pointer to the root TreeNode of the constructed tree.
func (f *IsolationForest) BuildTree(samples [][]float64, depth int) *TreeNode {
	numSamples := len(samples)
	if numSamples == 0 {
		return &TreeNode{}
	}
	numFeatures := len(samples[0])
	if depth >= f.MaxDepth || numSamples <= 1 {
		return &TreeNode{Size: numSamples}
	}

	splitIndex := rand.Intn(numFeatures)
	column := Column(samples, splitIndex)
	minValue, maxValue := MinMax(column)
	splitValue := rand.Float64()*(maxValue-minValue) + minValue

	leftSamples := make([][]float64, 0)
	rightSamples := make([][]float64, 0)
	for _, vector := range samples {
		if vector[splitIndex] < splitValue {
			leftSamples = append(leftSamples, vector)
		} else {
			rightSamples = append(rightSamples, vector)
		}
	}

	return &TreeNode{
		Left:       f.BuildTree(leftSamples, depth+1),
		Right:      f.BuildTree(rightSamples, depth+1),
		SplitIndex: splitIndex,
		SplitValue: splitValue,
	}
}

// Score computes anomaly scores for the given samples.
// Parameters:
//
//	samples - a Matrix of data points to compute scores for.
//
// Returns:
//
//	A slice of float64 values representing the anomaly score for each sample, where higher scores indicate greater anomaly likelihood.
//
// The anomaly score is based on the average path length of each sample across all trees.
func (f *IsolationForest) Score(samples [][]float64) []float64 {
	scores := make([]float64, len(samples))
	for i, sample := range samples {
		score := 0.0
		for _, tree := range f.Trees {
			score += pathLength(sample, tree, 0)
		}
		scores[i] = math.Pow(2.0, -score/float64(len(f.Trees))/averagePathLength(float64(f.SampleSize)))
	}
	return scores
}

// Predict identifies anomalies in the given samples based on the configured detection method.
// Parameters:
//
//	samples - a Matrix of data points to classify as normal or anomalous.
//
// Returns:
//
//	A slice of integers where 1 indicates an anomaly and 0 indicates a normal data point.
//
// This method uses the detection type specified in the options to determine the threshold for classifying anomalies.
func (f *IsolationForest) Predict(samples [][]float64) []int {
	predictions := make([]int, len(samples))
	scores := f.Score(samples)

	var threshold float64
	switch f.DetectionType {
	case DetectionTypeThreshold:
		threshold = f.Threshold
	case DetectionTypeProportion:
		threshold = Quantile(f.Score(samples), 1-f.Proportion)
	default:
		panic("Invalid detection type")
	}

	for i, score := range scores {
		if score >= threshold {
			predictions[i] = 1
		} else {
			predictions[i] = 0
		}
	}

	return predictions
}

// FeatureImportance computes the importance of features for a given sample.
// Parameters:
//
//	sample - a Vector representing the data point to compute feature importance for.
//
// Returns:
//
//	A slice of integers where each element represents the importance (frequency) of the corresponding feature.
//
// The importance is determined by how frequently each feature is used in the paths traversed by the sample across all trees.
func (f *IsolationForest) FeatureImportance(sample []float64) []int {
	importance := make([]int, len(sample))
	for _, tree := range f.Trees {
		for i, value := range tree.FeatureImportance(sample) {
			importance[i] += value
		}
	}
	return importance
}
