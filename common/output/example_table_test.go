package output_test

import (
	"os"

	"github.com/pgEdge/control-plane/common/output"
)

func ExampleTableFormatter() {
	formatter := output.NewTableFormatter(
		[]string{"Container ID", "image", "COMMAND", "created", "status", "poRts", "nAMEs"},
		[]string{`8bcedadc862f`, `registry:2`, `"/entrypoint.sh /etc…"`, `2 days ago`, `Up 2 days`, `5000/tcp`, `registry.1.mglc5falnmopy58xzx4m3r315`},
		[]string{`bcdf80e06726`, `moby/buildkit:buildx-stable-1`, `"/usr/bin/buildkitd-…"`, `2 months ago`, `Up 4 weeks`, ``, `buildx_buildkit_control-plane-ci0`},
	)
	formatter.AddRows(
		[]string{`2ac8ba3f8de3`, `70ecac72151f`, `"buildkitd --config …"`, `6 months ago`, `Up 4 weeks`, ``, `buildx_buildkit_control-plane0`},
		[]string{`dc652715170b`, `f618f93ec6d0`, `"buildkitd --config …"`, `7 months ago`, `Up 4 weeks`, ``, `buildx_buildkit_pgedge-images0`},
	)
	if err := formatter.Write(os.Stdout); err != nil {
		panic(err)
	}
	// output:
	// CONTAINER ID   IMAGE                           COMMAND                  CREATED        STATUS       PORTS      NAMES
	// 8bcedadc862f   registry:2                      "/entrypoint.sh /etc…"   2 days ago     Up 2 days    5000/tcp   registry.1.mglc5falnmopy58xzx4m3r315
	// bcdf80e06726   moby/buildkit:buildx-stable-1   "/usr/bin/buildkitd-…"   2 months ago   Up 4 weeks              buildx_buildkit_control-plane-ci0
	// 2ac8ba3f8de3   70ecac72151f                    "buildkitd --config …"   6 months ago   Up 4 weeks              buildx_buildkit_control-plane0
	// dc652715170b   f618f93ec6d0                    "buildkitd --config …"   7 months ago   Up 4 weeks              buildx_buildkit_pgedge-images0
}
