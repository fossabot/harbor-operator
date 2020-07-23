package harbor

import (
	"context"
	"fmt"

	"github.com/ovh/configstore"
	"github.com/sethvargo/go-password/password"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	goharborv1alpha2 "github.com/goharbor/harbor-operator/apis/goharbor.io/v1alpha2"
	"github.com/goharbor/harbor-operator/pkg/graph"
	"github.com/pkg/errors"
)

const (
	ConfigRegistryEncryptionCostKey = "registry-encryption-cost"
)

const (
	RegistryAuthRealm = "harbor-registry-basic-realm"
)

var (
	varTrue  = true
	varFalse = false
)

type RegistryAuthSecret graph.Resource

func (r *Reconciler) AddRegistryAuthenticationSecret(ctx context.Context, harbor *goharborv1alpha2.Harbor) (RegistryAuthSecret, error) {
	authSecret, err := r.GetRegistryAuthenticationSecret(ctx, harbor)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get secret")
	}

	authSecretRes, err := r.AddSecretToManage(ctx, authSecret)
	if err != nil {
		return nil, errors.Wrap(err, "cannot add secret")
	}

	return RegistryAuthSecret(authSecretRes), nil
}

func (r *Reconciler) AddRegistryConfigurations(ctx context.Context, harbor *goharborv1alpha2.Harbor) (RegistryAuthSecret, RegistryHTTPSecret, error) {
	authSecret, err := r.AddRegistryAuthenticationSecret(ctx, harbor)
	if err != nil {
		return nil, nil, errors.Wrap(err, "authentication secret")
	}

	httpSecret, err := r.AddRegistryHTTPSecret(ctx, harbor)
	if err != nil {
		return nil, nil, errors.Wrap(err, "http secret")
	}

	return authSecret, httpSecret, nil
}

type Registry graph.Resource

func (r *Reconciler) AddRegistry(ctx context.Context, harbor *goharborv1alpha2.Harbor, authSecret RegistryAuthSecret, httpSecret RegistryHTTPSecret) (Registry, error) {
	registry, err := r.GetRegistry(ctx, harbor)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get registry")
	}

	registryRes, err := r.AddBasicResource(ctx, registry, authSecret, httpSecret)
	if err != nil {
		return nil, errors.Wrap(err, "cannot add basic resource")
	}

	return Registry(registryRes), nil
}

type RegistryHTTPSecret graph.Resource

func (r *Reconciler) AddRegistryHTTPSecret(ctx context.Context, harbor *goharborv1alpha2.Harbor) (RegistryHTTPSecret, error) {
	httpSecret, err := r.GetRegistryHTTPSecret(ctx, harbor)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get secret")
	}

	httpSecretRes, err := r.AddSecretToManage(ctx, httpSecret)
	if err != nil {
		return nil, errors.Wrap(err, "cannot add secret")
	}

	return RegistryHTTPSecret(httpSecretRes), nil
}

const (
	// https://github.com/goharbor/harbor/blob/master/make/photon/prepare/utils/configs.py#L14
	RegistryAuthenticationUsername = "harbor_registry_user"

	RegistryAuthenticationPasswordLength      = 32
	RegistryAuthenticationPasswordNumDigits   = 10
	RegistryAuthenticationPasswordNumSpecials = 10
)

func (r *Reconciler) GetRegistryAuthenticationSecret(ctx context.Context, harbor *goharborv1alpha2.Harbor) (*corev1.Secret, error) {
	name := r.NormalizeName(ctx, harbor.GetName(), "registry", "basicauth")
	namespace := harbor.GetNamespace()

	password, err := password.Generate(RegistryAuthenticationPasswordLength, RegistryAuthenticationPasswordNumDigits, RegistryAuthenticationPasswordNumSpecials, false, true)
	if err != nil {
		return nil, errors.Wrap(err, "cannot generate password")
	}

	cost, err := r.ConfigStore.GetItemValueInt(ConfigRegistryEncryptionCostKey)
	if err != nil {
		if _, ok := err.(configstore.ErrItemNotFound); !ok {
			return nil, errors.Wrap(err, "cannot get encryption cost")
		}

		cost = int64(bcrypt.DefaultCost)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), int(cost))
	if err != nil {
		return nil, errors.Wrap(err, "cannot encrypt password")
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Immutable: &varFalse,
		Type:      goharborv1alpha2.SecretTypeHTPasswd,
		StringData: map[string]string{
			goharborv1alpha2.HTPasswdFileName: fmt.Sprintf("%s:%s", RegistryAuthenticationUsername, string(hashedPassword)),
			goharborv1alpha2.SharedSecretKey:  password,
		},
	}, nil
}

const (
	RegistrySecretPasswordLength      = 128
	RegistrySecretPasswordNumDigits   = 16
	RegistrySecretPasswordNumSpecials = 48
)

func (r *Reconciler) GetRegistryHTTPSecret(ctx context.Context, harbor *goharborv1alpha2.Harbor) (*corev1.Secret, error) {
	name := r.NormalizeName(ctx, harbor.GetName(), "registry", "http")
	namespace := harbor.GetNamespace()

	secret, err := password.Generate(RegistrySecretPasswordLength, RegistrySecretPasswordNumDigits, RegistrySecretPasswordNumSpecials, false, true)
	if err != nil {
		return nil, errors.Wrap(err, "cannot generate secret")
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Immutable: &varTrue,
		Type:      goharborv1alpha2.SecretTypeSingle,
		StringData: map[string]string{
			goharborv1alpha2.SharedSecretKey: secret,
		},
	}, nil
}

func (r *Reconciler) GetRegistry(ctx context.Context, harbor *goharborv1alpha2.Harbor) (*goharborv1alpha2.Registry, error) {
	name := r.NormalizeName(ctx, harbor.GetName())
	namespace := harbor.GetNamespace()

	authenticationSecretName := r.NormalizeName(ctx, harbor.GetName(), "registry", "basicauth")
	httpSecretName := r.NormalizeName(ctx, harbor.GetName(), "registry", "http")

	redisDSN, err := harbor.Spec.RedisDSN(goharborv1alpha2.RegistryRedis)
	if err != nil {
		return nil, errors.Wrap(err, "redis")
	}

	return &goharborv1alpha2.Registry{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: goharborv1alpha2.RegistrySpec{
			ComponentSpec: harbor.Spec.Registry.ComponentSpec,
			RegistryConfig01: goharborv1alpha2.RegistryConfig01{
				Log: goharborv1alpha2.RegistryLogSpec{
					AccessLog: goharborv1alpha2.RegistryAccessLogSpec{
						Disabled: false,
					},
					Level: harbor.Spec.LogLevel.Registry(),
				},
				Authentication: goharborv1alpha2.RegistryAuthenticationSpec{
					HTPasswd: &goharborv1alpha2.RegistryAuthenticationHTPasswdSpec{
						Realm:     RegistryAuthRealm,
						SecretRef: authenticationSecretName,
					},
				},
				Validation: goharborv1alpha2.RegistryValidationSpec{
					Disabled: true,
				},
				Middlewares: goharborv1alpha2.RegistryMiddlewaresSpec{
					Storage: harbor.Spec.Registry.StorageMiddlewares,
				},
				HTTP: goharborv1alpha2.RegistryHTTPSpec{
					RelativeURLs: harbor.Spec.Registry.RelativeURLs,
					SecretRef:    httpSecretName,
				},
				Storage: goharborv1alpha2.RegistryStorageSpec{
					Driver: harbor.Spec.Persistence.ImageChartStorage.Registry(),
					Cache: goharborv1alpha2.RegistryStorageCacheSpec{
						Blobdescriptor: "redis",
					},
					Redirect: harbor.Spec.Persistence.ImageChartStorage.Redirect,
				},
				Redis: &goharborv1alpha2.RegistryRedisSpec{
					OpacifiedDSN: *redisDSN,
				},
			},
		},
	}, nil
}