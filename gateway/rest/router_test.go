package rest

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeVersion "k8s.io/apimachinery/pkg/version"

	sdk "github.com/cosmos/cosmos-sdk/types"

	manifestValidation "github.com/akash-network/akash-api/go/manifest/v2beta2"
	qmock "github.com/akash-network/akash-api/go/node/client/v1beta2/mocks"
	dtypes "github.com/akash-network/akash-api/go/node/deployment/v1beta3"
	mtypes "github.com/akash-network/akash-api/go/node/market/v1beta4"
	types "github.com/akash-network/akash-api/go/node/market/v1beta4"
	"github.com/akash-network/akash-api/go/testutil"

	"github.com/akash-network/node/sdl"

	"github.com/akash-network/provider"
	kubeclienterrors "github.com/akash-network/provider/cluster/kube/errors"
	pcmock "github.com/akash-network/provider/cluster/mocks"
	clustertypes "github.com/akash-network/provider/cluster/types/v1beta3"
	ctypes "github.com/akash-network/provider/cluster/types/v1beta3"
	clmocks "github.com/akash-network/provider/cluster/types/v1beta3/mocks"
	pmmock "github.com/akash-network/provider/manifest/mocks"
	pmock "github.com/akash-network/provider/mocks"
	"github.com/akash-network/provider/pkg/apis/akash.network/v2beta2"
	"github.com/akash-network/provider/version"
)

const (
	testSDL     = "../../testdata/sdl/simple.yaml"
	serviceName = "database"
)

var errGeneric = errors.New("generic test error")

type fakeKubernetesStatusError struct {
	status metav1.Status
}

func (fkse fakeKubernetesStatusError) Status() metav1.Status {
	return fkse.status
}

func (fkse fakeKubernetesStatusError) Error() string {
	return "fake error"
}

type routerTest struct {
	caddr          sdk.Address
	paddr          sdk.Address
	pmclient       *pmmock.Client
	pcclient       *pcmock.Client
	pclient        *pmock.Client
	qclient        *qmock.QueryClient
	clusterService *pcmock.Service
	hostnameClient *clmocks.HostnameServiceClient
	gwclient       *client
	ccert          testutil.TestCertificate
	pcert          testutil.TestCertificate
	host           *url.URL
}

// TODO - add some tests in here to make sure the IP operator calls work as intended

func runRouterTest(t *testing.T, authClient bool, fn func(*routerTest)) {
	t.Helper()

	mocks := createMocks()

	mf := &routerTest{
		caddr:          testutil.AccAddress(t),
		paddr:          testutil.AccAddress(t),
		pmclient:       mocks.pmclient,
		pcclient:       mocks.pcclient,
		pclient:        mocks.pclient,
		qclient:        mocks.qclient,
		hostnameClient: mocks.hostnameClient,
		clusterService: mocks.clusterService,
	}

	mf.ccert = testutil.Certificate(t, mf.caddr, testutil.CertificateOptionMocks(mocks.qclient))
	mf.pcert = testutil.Certificate(
		t,
		mf.paddr,
		testutil.CertificateOptionDomains([]string{"localhost", "127.0.0.1"}),
		testutil.CertificateOptionMocks(mocks.qclient))

	var certs []tls.Certificate
	if authClient {
		certs = mf.ccert.Cert
	}

	withServer(t, mf.paddr, mocks.pclient, mocks.qclient, mf.pcert.Cert, func(host string) {
		var err error
		mf.host, err = url.Parse(host)
		require.NoError(t, err)

		gclient, err := NewClient(context.Background(), mocks.qclient, mf.paddr, certs)
		require.NoError(t, err)
		require.NotNil(t, gclient)

		mf.gwclient = gclient.(*client)

		fn(mf)
	})
}

func testCertHelper(t *testing.T, test *routerTest) {
	test.pmclient.On(
		"Submit",
		mock.Anything,
		mock.AnythingOfType("dtypes.DeploymentID"),
		mock.AnythingOfType("v2beta2.Manifest"),
	).Return(nil)

	dseq := uint64(testutil.RandRangeInt(1, 1000)) // nolint: gosec

	uri, err := makeURI(test.host, submitManifestPath(dseq))
	require.NoError(t, err)

	sdl, err := sdl.ReadFile(testSDL)
	require.NoError(t, err)

	mani, err := sdl.Manifest()
	require.NoError(t, err)

	buf, err := json.Marshal(mani)
	require.NoError(t, err)

	req, err := http.NewRequest("PUT", uri, bytes.NewBuffer(buf))
	require.NoError(t, err)

	req.Header.Set("Content-Type", contentTypeJSON)

	rCl := test.gwclient.newReqClient(context.Background())
	_, err = rCl.hclient.Do(req)
	require.Error(t, err)
	// return error message looks like
	// Put "https://127.0.0.1:58536/deployment/652/manifest": tls: unable to verify certificate: x509: cannot validate certificate for 127.0.0.1 because it doesn't contain any IP SANs
	require.Regexp(t, `^(Put|Get) (".*": )tls: unable to verify certificate: \(.*\)$`, err.Error())
}

func TestRouteNotActiveClientCert(t *testing.T) {
	mocks := createMocks()

	mf := &routerTest{
		caddr:    testutil.AccAddress(t),
		paddr:    testutil.AccAddress(t),
		pmclient: mocks.pmclient,
		pcclient: mocks.pcclient,
		pclient:  mocks.pclient,
		qclient:  mocks.qclient,
	}

	mf.ccert = testutil.Certificate(
		t,
		mf.caddr,
		testutil.CertificateOptionMocks(mocks.qclient),
		testutil.CertificateOptionNotBefore(time.Now().Add(time.Hour*24)),
	)
	mf.pcert = testutil.Certificate(t, mf.paddr, testutil.CertificateOptionMocks(mocks.qclient))

	withServer(t, mf.paddr, mocks.pclient, mocks.qclient, mf.pcert.Cert, func(host string) {
		var err error
		mf.host, err = url.Parse(host)
		require.NoError(t, err)

		gclient, err := NewClient(context.Background(), mocks.qclient, mf.paddr, mf.ccert.Cert)
		require.NoError(t, err)
		require.NotNil(t, gclient)

		mf.gwclient = gclient.(*client)

		testCertHelper(t, mf)
	})
}

func TestRouteExpiredClientCert(t *testing.T) {
	mocks := createMocks()

	mf := &routerTest{
		caddr:    testutil.AccAddress(t),
		paddr:    testutil.AccAddress(t),
		pmclient: mocks.pmclient,
		pcclient: mocks.pcclient,
		pclient:  mocks.pclient,
		qclient:  mocks.qclient,
	}

	mf.ccert = testutil.Certificate(
		t,
		mf.caddr,
		testutil.CertificateOptionMocks(mocks.qclient),
		testutil.CertificateOptionNotBefore(time.Now().Add(time.Hour*(-48))),
		testutil.CertificateOptionNotAfter(time.Now().Add(time.Hour*(-24))),
	)
	mf.pcert = testutil.Certificate(t, mf.paddr, testutil.CertificateOptionMocks(mocks.qclient))

	withServer(t, mf.paddr, mocks.pclient, mocks.qclient, mf.pcert.Cert, func(host string) {
		var err error
		mf.host, err = url.Parse(host)
		require.NoError(t, err)

		gclient, err := NewClient(context.Background(), mocks.qclient, mf.paddr, mf.ccert.Cert)
		require.NoError(t, err)
		require.NotNil(t, gclient)

		mf.gwclient = gclient.(*client)

		testCertHelper(t, mf)
	})
}

func TestRouteNotActiveServerCert(t *testing.T) {
	mocks := createMocks()

	mf := &routerTest{
		caddr:    testutil.AccAddress(t),
		paddr:    testutil.AccAddress(t),
		pmclient: mocks.pmclient,
		pcclient: mocks.pcclient,
		pclient:  mocks.pclient,
		qclient:  mocks.qclient,
	}

	mf.ccert = testutil.Certificate(
		t,
		mf.caddr,
		testutil.CertificateOptionMocks(mocks.qclient),
	)
	mf.pcert = testutil.Certificate(
		t,
		mf.paddr,
		testutil.CertificateOptionMocks(mocks.qclient),
		testutil.CertificateOptionNotBefore(time.Now().Add(time.Hour*24)),
	)

	withServer(t, mf.paddr, mocks.pclient, mocks.qclient, mf.pcert.Cert, func(host string) {
		var err error
		mf.host, err = url.Parse(host)
		require.NoError(t, err)

		gclient, err := NewClient(context.Background(), mocks.qclient, mf.paddr, mf.ccert.Cert)
		require.NoError(t, err)
		require.NotNil(t, gclient)

		mf.gwclient = gclient.(*client)

		testCertHelper(t, mf)
	})
}

func TestRouteExpiredServerCert(t *testing.T) {
	mocks := createMocks()

	mf := &routerTest{
		caddr:    testutil.AccAddress(t),
		paddr:    testutil.AccAddress(t),
		pmclient: mocks.pmclient,
		pcclient: mocks.pcclient,
		pclient:  mocks.pclient,
		qclient:  mocks.qclient,
	}

	mf.ccert = testutil.Certificate(
		t,
		mf.caddr,
		testutil.CertificateOptionMocks(mocks.qclient),
	)
	mf.pcert = testutil.Certificate(
		t,
		mf.paddr,
		testutil.CertificateOptionMocks(mocks.qclient),
		testutil.CertificateOptionNotBefore(time.Now().Add(time.Hour*(-48))),
		testutil.CertificateOptionNotAfter(time.Now().Add(time.Hour*(-24))),
	)

	withServer(t, mf.paddr, mocks.pclient, mocks.qclient, mf.pcert.Cert, func(host string) {
		var err error
		mf.host, err = url.Parse(host)
		require.NoError(t, err)

		gclient, err := NewClient(context.Background(), mocks.qclient, mf.paddr, mf.ccert.Cert)
		require.NoError(t, err)
		require.NotNil(t, gclient)

		mf.gwclient = gclient.(*client)

		testCertHelper(t, mf)
	})
}

func TestRouteDoesNotExist(t *testing.T) {
	runRouterTest(t, false, func(test *routerTest) {
		uri, err := makeURI(test.host, "foobar")
		require.NoError(t, err)

		req, err := http.NewRequest("GET", uri, nil)
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)

		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)

		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestRouteVersionOK(t *testing.T) {
	runRouterTest(t, false, func(test *routerTest) {
		// these are set at build time
		version.Version = "akashTest"
		version.Commit = "testCommit"
		version.BuildTags = "testTags"

		status := versionInfo{
			Akash: version.Info{
				Version:          "akashTest",
				GitCommit:        "testCommit",
				BuildTags:        "testTags",
				GoVersion:        "", // ignored in comparison
				CosmosSdkVersion: "", // ignored in comparison
			},
			Kube: &kubeVersion.Info{
				Major:        "1",
				Minor:        "2",
				GitVersion:   "3",
				GitCommit:    "4",
				GitTreeState: "5",
				BuildDate:    "6",
				GoVersion:    "7",
				Compiler:     "8",
				Platform:     "9",
			},
		}

		test.pcclient.On("KubeVersion").Return(status.Kube, nil)

		uri, err := makeURI(test.host, versionPath())
		require.NoError(t, err)

		req, err := http.NewRequest("GET", uri, nil)
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)

		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var data versionInfo
		decoder := json.NewDecoder(resp.Body)
		err = decoder.Decode(&data)
		require.NoError(t, err)
		require.Equal(t, status.Kube, data.Kube)
		require.Equal(t, status.Akash.Version, data.Akash.Version)
		require.Equal(t, status.Akash.GitCommit, data.Akash.GitCommit)
		require.Equal(t, status.Akash.BuildTags, data.Akash.BuildTags)
	})
}

func TestRouteStatusOK(t *testing.T) {
	runRouterTest(t, false, func(test *routerTest) {
		status := &provider.Status{
			Cluster:               nil,
			Bidengine:             nil,
			Manifest:              nil,
			ClusterPublicHostname: "foobar",
		}

		test.pclient.On("Status", mock.Anything).Return(status, nil)

		uri, err := makeURI(test.host, statusPath())
		require.NoError(t, err)

		req, err := http.NewRequest("GET", uri, nil)
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)

		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		data := make(map[string]interface{})
		decoder := json.NewDecoder(resp.Body)
		err = decoder.Decode(&data)
		require.NoError(t, err)
		cph, ok := data["cluster_public_hostname"].(string)
		require.True(t, ok)
		require.Equal(t, cph, "foobar")
	})
}

func TestRouteStatusFails(t *testing.T) {
	runRouterTest(t, false, func(test *routerTest) {
		test.pclient.On("Status", mock.Anything).Return(nil, errGeneric)

		uri, err := makeURI(test.host, statusPath())
		require.NoError(t, err)

		req, err := http.NewRequest("GET", uri, nil)
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)
		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)

		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Regexp(t, "^generic test error(?s:.)*$", string(data))
	})
}

func TestRouteValidateOK(t *testing.T) {
	runRouterTest(t, true, func(test *routerTest) {
		validate := provider.ValidateGroupSpecResult{
			MinBidPrice: testutil.AkashDecCoin(t, 200),
		}

		test.pclient.On("Validate", mock.Anything, mock.Anything, mock.Anything).Return(validate, nil)

		uri, err := makeURI(test.host, validatePath())
		require.NoError(t, err)

		gspec := testutil.GroupSpec(t)
		bgspec, err := json.Marshal(&gspec)
		require.NoError(t, err)

		req, err := http.NewRequest("GET", uri, bytes.NewReader(bgspec))
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)

		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		data := make(map[string]interface{})
		decoder := json.NewDecoder(resp.Body)
		err = decoder.Decode(&data)
		require.NoError(t, err)
	})
}

func TestRouteValidateUnauthorized(t *testing.T) {
	runRouterTest(t, false, func(test *routerTest) {
		validate := provider.ValidateGroupSpecResult{
			MinBidPrice: testutil.AkashDecCoin(t, 200),
		}

		test.pclient.On("Validate", mock.Anything, mock.Anything, mock.Anything).Return(validate, nil)

		uri, err := makeURI(test.host, validatePath())
		require.NoError(t, err)

		gspec := testutil.GroupSpec(t)
		bgspec, err := json.Marshal(&gspec)
		require.NoError(t, err)

		req, err := http.NewRequest("GET", uri, bytes.NewReader(bgspec))
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)

		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}

func TestRouteValidateFails(t *testing.T) {
	runRouterTest(t, true, func(test *routerTest) {
		test.pclient.On("Validate", mock.Anything, mock.Anything, mock.Anything).Return(provider.ValidateGroupSpecResult{}, errGeneric)

		uri, err := makeURI(test.host, validatePath())
		require.NoError(t, err)

		gspec := testutil.GroupSpec(t)
		bgspec, err := json.Marshal(&gspec)
		require.NoError(t, err)

		req, err := http.NewRequest("GET", uri, bytes.NewReader(bgspec))
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)
		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)

		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Regexp(t, "^generic test error(?s:.)*$", string(data))
	})
}

func TestRouteValidateFailsEmptyBody(t *testing.T) {
	runRouterTest(t, true, func(test *routerTest) {
		test.pclient.On("Validate", mock.Anything, mock.Anything).Return(provider.ValidateGroupSpecResult{}, errGeneric)

		uri, err := makeURI(test.host, validatePath())
		require.NoError(t, err)

		req, err := http.NewRequest("GET", uri, nil)
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)
		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)

		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Regexp(t, "empty payload", string(data))
	})
}

func TestRoutePutManifestOK(t *testing.T) {
	runRouterTest(t, true, func(test *routerTest) {
		dseq := uint64(testutil.RandRangeInt(1, 1000)) // nolint: gosec
		test.pmclient.On(
			"Submit",
			mock.Anything,
			dtypes.DeploymentID{
				Owner: test.caddr.String(),
				DSeq:  dseq,
			},
			mock.AnythingOfType("v2beta2.Manifest"),
		).Return(nil)

		uri, err := makeURI(test.host, submitManifestPath(dseq))
		require.NoError(t, err)

		sdl, err := sdl.ReadFile(testSDL)
		require.NoError(t, err)

		mani, err := sdl.Manifest()
		require.NoError(t, err)

		buf, err := json.Marshal(mani)
		require.NoError(t, err)

		req, err := http.NewRequest("PUT", uri, bytes.NewBuffer(buf))
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)

		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, string(data), "")
	})
}

func TestRoutePutInvalidManifest(t *testing.T) {
	_ = dtypes.DeploymentID{}
	runRouterTest(t, true, func(test *routerTest) {
		dseq := uint64(testutil.RandRangeInt(1, 1000)) // nolint: gosec
		test.pmclient.On("Submit",
			mock.Anything,
			dtypes.DeploymentID{
				Owner: test.caddr.String(),
				DSeq:  dseq,
			},

			mock.AnythingOfType("v2beta2.Manifest"),
		).Return(manifestValidation.ErrInvalidManifest)

		uri, err := makeURI(test.host, submitManifestPath(dseq))
		require.NoError(t, err)

		sdl, err := sdl.ReadFile(testSDL)
		require.NoError(t, err)

		mani, err := sdl.Manifest()
		require.NoError(t, err)

		buf, err := json.Marshal(mani)
		require.NoError(t, err)

		req, err := http.NewRequest("PUT", uri, bytes.NewBuffer(buf))
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)

		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)

		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Regexp(t, "^invalid manifest(?s:.)*$", string(data))
	})
}

func mockManifestGroupsForRouterTest(rt *routerTest, leaseID mtypes.LeaseID) {
	status := make(map[string]*ctypes.ServiceStatus)
	status[testServiceName] = &ctypes.ServiceStatus{
		Name:               testServiceName,
		Available:          8,
		Total:              8,
		URIs:               nil,
		ObservedGeneration: 0,
		Replicas:           0,
		UpdatedReplicas:    0,
		ReadyReplicas:      0,
		AvailableReplicas:  0,
	}
	rt.pcclient.On("LeaseStatus", mock.Anything, leaseID).Return(status, nil)
	rt.pcclient.On("GetManifestGroup", mock.Anything, leaseID).Return(true, v2beta2.ManifestGroup{
		Name: testGroupName,
		Services: []v2beta2.ManifestService{{
			Name:  testServiceName,
			Image: testImageName,
			Args:  nil,
			Env:   nil,
			Resources: v2beta2.Resources{
				CPU: v2beta2.ResourceCPU{
					Units: 1000,
				},
				Memory: v2beta2.ResourceMemory{
					Size: "3333",
				},
				Storage: v2beta2.ResourceStorage{
					{
						Name: "default",
						Size: "4444",
					},
				},
			},
			Count: 1,
			Expose: []v2beta2.ManifestServiceExpose{{
				Port:         8080,
				ExternalPort: 80,
				Proto:        "TCP",
				Service:      testServiceName,
				Global:       true,
				Hosts:        []string{"hello.localhost"},
				HTTPOptions: v2beta2.ManifestServiceExposeHTTPOptions{
					MaxBodySize: 1,
					ReadTimeout: 2,
					SendTimeout: 3,
					NextTries:   4,
					NextTimeout: 5,
					NextCases:   nil,
				},
				IP:                     "",
				EndpointSequenceNumber: 1,
			}},
			Params: nil,
		}},
	}, nil)
}

func TestRouteLeaseStatusOk(t *testing.T) {
	runRouterTest(t, true, func(test *routerTest) {
		leaseID := testutil.LeaseID(t)
		leaseID.Owner = test.caddr.String()
		leaseID.Provider = test.paddr.String()
		mockManifestGroupsForRouterTest(test, leaseID)

		uri, err := makeURI(test.host, leaseStatusPath(leaseID))
		require.NoError(t, err)

		parsedSDL, err := sdl.ReadFile(testSDL)
		require.NoError(t, err)

		mani, err := parsedSDL.Manifest()
		require.NoError(t, err)

		buf, err := json.Marshal(mani)
		require.NoError(t, err)

		req, err := http.NewRequest("GET", uri, bytes.NewBuffer(buf))
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)

		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		data := make(map[string]interface{})
		dec := json.NewDecoder(resp.Body)
		err = dec.Decode(&data)
		require.NoError(t, err)
	})
}

func TestRouteLeaseNotInKubernetes(t *testing.T) {
	runRouterTest(t, true, func(test *routerTest) {
		leaseID := testutil.LeaseID(t)
		leaseID.Owner = test.caddr.String()
		leaseID.Provider = test.paddr.String()

		kubeStatus := fakeKubernetesStatusError{
			status: metav1.Status{
				TypeMeta: metav1.TypeMeta{},
				ListMeta: metav1.ListMeta{},
				Status:   "",
				Message:  "",
				Reason:   metav1.StatusReasonNotFound,
				Details:  nil,
				Code:     0,
			},
		}
		test.pcclient.On("LeaseStatus", mock.Anything, leaseID).Return(nil, kubeStatus)
		mockManifestGroupsForRouterTest(test, leaseID)

		uri, err := makeURI(test.host, leaseStatusPath(leaseID))
		require.NoError(t, err)

		parsedSDL, err := sdl.ReadFile(testSDL)
		require.NoError(t, err)

		mani, err := parsedSDL.Manifest()
		require.NoError(t, err)

		buf, err := json.Marshal(mani)
		require.NoError(t, err)

		req, err := http.NewRequest("GET", uri, bytes.NewBuffer(buf))
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)

		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestRouteLeaseStatusErr(t *testing.T) {
	runRouterTest(t, true, func(test *routerTest) {
		leaseID := testutil.LeaseID(t)
		leaseID.Owner = test.caddr.String()
		leaseID.Provider = test.paddr.String()
		test.pcclient.On("LeaseStatus", mock.Anything, leaseID).Return(nil, errGeneric)
		mockManifestGroupsForRouterTest(test, leaseID)

		uri, err := makeURI(test.host, leaseStatusPath(leaseID))
		require.NoError(t, err)

		req, err := http.NewRequest("GET", uri, nil)
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)

		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)

		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Regexp(t, "^generic test error(?s:.)*$", string(data))
	})
}

func TestRouteServiceStatusOK(t *testing.T) {
	runRouterTest(t, true, func(test *routerTest) {
		dseq := uint64(testutil.RandRangeInt(1, 1000))    // nolint: gosec
		oseq := uint32(testutil.RandRangeInt(2000, 3000)) // nolint: gosec
		gseq := uint32(testutil.RandRangeInt(4000, 5000)) // nolint: gosec

		status := &clustertypes.ServiceStatus{
			Name:               "",
			Available:          0,
			Total:              0,
			URIs:               nil,
			ObservedGeneration: 0,
			Replicas:           0,
			UpdatedReplicas:    0,
			ReadyReplicas:      0,
			AvailableReplicas:  0,
		}
		test.pcclient.On("ServiceStatus", mock.Anything, types.LeaseID{
			Owner:    test.caddr.String(),
			DSeq:     dseq,
			GSeq:     gseq,
			OSeq:     oseq,
			Provider: test.paddr.String(),
		}, serviceName).Return(status, nil)

		lid := types.LeaseID{
			DSeq:     dseq,
			GSeq:     gseq,
			OSeq:     oseq,
			Provider: test.paddr.String(),
		}

		uri, err := makeURI(test.host, serviceStatusPath(lid, serviceName))
		require.NoError(t, err)

		req, err := http.NewRequest("GET", uri, nil)
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)

		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)

		require.Equal(t, http.StatusOK, resp.StatusCode)
		data := make(map[string]interface{})
		dec := json.NewDecoder(resp.Body)
		err = dec.Decode(&data)
		require.NoError(t, err)
	})
}

func TestRouteServiceStatusNoDeployment(t *testing.T) {
	runRouterTest(t, true, func(test *routerTest) {
		dseq := uint64(testutil.RandRangeInt(1, 1000))    // nolint: gosec
		oseq := uint32(testutil.RandRangeInt(2000, 3000)) // nolint: gosec
		gseq := uint32(testutil.RandRangeInt(4000, 5000)) // nolint: gosec

		test.pcclient.On("ServiceStatus", mock.Anything, types.LeaseID{
			Owner:    test.caddr.String(),
			DSeq:     dseq,
			GSeq:     gseq,
			OSeq:     oseq,
			Provider: test.paddr.String(),
		}, serviceName).Return(nil, kubeclienterrors.ErrNoDeploymentForLease)

		lid := types.LeaseID{
			DSeq:     dseq,
			GSeq:     gseq,
			OSeq:     oseq,
			Provider: test.paddr.String(),
		}

		uri, err := makeURI(test.host, serviceStatusPath(lid, serviceName))
		require.NoError(t, err)

		req, err := http.NewRequest("GET", uri, nil)
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)

		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)

		require.Equal(t, http.StatusNotFound, resp.StatusCode)
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Regexp(t, "^kube: no deployment(?s:.)*$", string(data))
	})
}

func TestRouteServiceStatusKubernetesNotFound(t *testing.T) {
	runRouterTest(t, true, func(test *routerTest) {
		dseq := uint64(testutil.RandRangeInt(1, 1000))    // nolint: gosec
		oseq := uint32(testutil.RandRangeInt(2000, 3000)) // nolint: gosec
		gseq := uint32(testutil.RandRangeInt(4000, 5000)) // nolint: gosec

		kubeStatus := fakeKubernetesStatusError{
			status: metav1.Status{
				TypeMeta: metav1.TypeMeta{},
				ListMeta: metav1.ListMeta{},
				Status:   "",
				Message:  "",
				Reason:   metav1.StatusReasonNotFound,
				Details:  nil,
				Code:     0,
			},
		}

		test.pcclient.On("ServiceStatus", mock.Anything, types.LeaseID{
			Owner:    test.caddr.String(),
			DSeq:     dseq,
			GSeq:     gseq,
			OSeq:     oseq,
			Provider: test.paddr.String(),
		}, serviceName).Return(nil, kubeStatus)

		lid := types.LeaseID{
			DSeq:     dseq,
			GSeq:     gseq,
			OSeq:     oseq,
			Provider: test.paddr.String(),
		}

		uri, err := makeURI(test.host, serviceStatusPath(lid, serviceName))
		require.NoError(t, err)

		req, err := http.NewRequest("GET", uri, nil)
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)

		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)

		require.Equal(t, http.StatusNotFound, resp.StatusCode)
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Regexp(t, "^fake error(?s:.)*$", string(data))
	})
}

func TestRouteServiceStatusError(t *testing.T) {
	runRouterTest(t, true, func(test *routerTest) {
		dseq := uint64(testutil.RandRangeInt(1, 1000))    // nolint: gosec
		oseq := uint32(testutil.RandRangeInt(2000, 3000)) // nolint: gosec
		gseq := uint32(testutil.RandRangeInt(4000, 5000)) // nolint: gosec

		test.pcclient.On("ServiceStatus", mock.Anything, types.LeaseID{
			Owner:    test.caddr.String(),
			DSeq:     dseq,
			GSeq:     gseq,
			OSeq:     oseq,
			Provider: test.paddr.String(),
		}, serviceName).Return(nil, errGeneric)

		lid := types.LeaseID{
			DSeq:     dseq,
			GSeq:     gseq,
			OSeq:     oseq,
			Provider: test.paddr.String(),
		}

		uri, err := makeURI(test.host, serviceStatusPath(lid, serviceName))
		require.NoError(t, err)

		req, err := http.NewRequest("GET", uri, nil)
		require.NoError(t, err)

		req.Header.Set("Content-Type", contentTypeJSON)

		rCl := test.gwclient.newReqClient(context.Background())
		resp, err := rCl.hclient.Do(req)
		require.NoError(t, err)

		require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Regexp(t, "^generic test error(?s:.)*$", string(data))
	})
}
