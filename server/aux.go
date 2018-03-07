package main

import "math"

// Computes the dot product of the two input arrays
func dotProduct(coefficients []float64, features []float64) float64 {
	// Check that the coefficients and features are of the same size
	if len(coefficients) != len(features) {
		panic("Input coefficients and input features are of varying size") // Can modify to handle gracefully
	}

	var prod float64 = 0.0
	for i := 0; i < len(coefficients); i++ {
		prod += coefficients[i] * features[i]
	}

	return prod
}

// Canonical logistic link function
func logistic(dotprod float64) float64 {
	return 1 / (1 + math.Exp(-1*dotprod))
}

// Responsible for mapping the input features and corresponding coefficients to a probability
func featureToProb(coefficients []float64, features []float64) float64 {
	dotProd := dotProduct(coefficients, features)
	prob := logistic(dotProd)
	return prob
}
