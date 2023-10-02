// Package classification awesome.
//
// Documentation of Skupper flow-collector API.
//
//     Schemes: https
//     BasePath: /api/v1alpha1
//     Version: 0.0.1
//     Host: skupper-console-host
//
//     Consumes:
//     - application/json
//
//     Produces:
//     - application/json
//
//     Security:
//     - basic
//
//    SecurityDefinitions:
//    basic:
//      type: basic
//
// swagger:meta
package docs


func init() {
	swag.Register(SwaggerInfo.InstanceName(), SwaggerInfo)
}
