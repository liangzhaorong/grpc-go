/*
 *
 * Copyright 2022 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"regexp"

	gcplogging "cloud.google.com/go/logging"
	"golang.org/x/oauth2/google"
	configpb "google.golang.org/grpc/gcp/observability/internal/config"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	envObservabilityConfig     = "GRPC_CONFIG_OBSERVABILITY"
	envObservabilityConfigJSON = "GRPC_CONFIG_OBSERVABILITY_JSON"
	envProjectID               = "GOOGLE_CLOUD_PROJECT"
	logFilterPatternRegexpStr  = `^([\w./]+)/((?:\w+)|[*])$`
)

var logFilterPatternRegexp = regexp.MustCompile(logFilterPatternRegexpStr)

// fetchDefaultProjectID fetches the default GCP project id from environment.
func fetchDefaultProjectID(ctx context.Context) string {
	// Step 1: Check ENV var
	if s := os.Getenv(envProjectID); s != "" {
		logger.Infof("Found project ID from env %v: %v", envProjectID, s)
		return s
	}
	// Step 2: Check default credential
	credentials, err := google.FindDefaultCredentials(ctx, gcplogging.WriteScope)
	if err != nil {
		logger.Infof("Failed to locate Google Default Credential: %v", err)
		return ""
	}
	if credentials.ProjectID == "" {
		logger.Infof("Failed to find project ID in default credential: %v", err)
		return ""
	}
	logger.Infof("Found project ID from Google Default Credential: %v", credentials.ProjectID)
	return credentials.ProjectID
}

func validateFilters(config *configpb.ObservabilityConfig) error {
	for _, filter := range config.GetLogFilters() {
		if filter.Pattern == "*" {
			continue
		}
		match := logFilterPatternRegexp.FindStringSubmatch(filter.Pattern)
		if match == nil {
			return fmt.Errorf("invalid log filter pattern: %v", filter.Pattern)
		}
	}
	return nil
}

// unmarshalAndVerifyConfig unmarshals a json string representing an
// observability config into its protobuf format, and also verifies the
// configuration's fields for validity.
func unmarshalAndVerifyConfig(rawJSON json.RawMessage) (*configpb.ObservabilityConfig, error) {
	var config configpb.ObservabilityConfig
	if err := protojson.Unmarshal(rawJSON, &config); err != nil {
		return nil, fmt.Errorf("error parsing observability config: %v", err)
	}
	if err := validateFilters(&config); err != nil {
		return nil, fmt.Errorf("error parsing observability config: %v", err)
	}
	if config.GlobalTraceSamplingRate > 1 || config.GlobalTraceSamplingRate < 0 {
		return nil, fmt.Errorf("error parsing observability config: invalid global trace sampling rate %v", config.GlobalTraceSamplingRate)
	}
	logger.Infof("Parsed ObservabilityConfig: %+v", &config)
	return &config, nil
}

func parseObservabilityConfig() (*configpb.ObservabilityConfig, error) {
	if fileSystemPath := os.Getenv(envObservabilityConfigJSON); fileSystemPath != "" {
		content, err := ioutil.ReadFile(fileSystemPath) // TODO: Switch to os.ReadFile once dropped support for go 1.15
		if err != nil {
			return nil, fmt.Errorf("error reading observability configuration file %q: %v", fileSystemPath, err)
		}
		return unmarshalAndVerifyConfig(content)
	} else if content := os.Getenv(envObservabilityConfig); content != "" {
		return unmarshalAndVerifyConfig([]byte(content))
	}
	// If the ENV var doesn't exist, do nothing
	return nil, nil
}

func ensureProjectIDInObservabilityConfig(ctx context.Context, config *configpb.ObservabilityConfig) error {
	if config.GetDestinationProjectId() == "" {
		// Try to fetch the GCP project id
		projectID := fetchDefaultProjectID(ctx)
		if projectID == "" {
			return fmt.Errorf("empty destination project ID")
		}
		config.DestinationProjectId = projectID
	}
	return nil
}
