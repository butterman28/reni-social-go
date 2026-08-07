import os
import re


def main():
    routes_file = "internal/routes/routes.go"
    if not os.path.exists(routes_file):
        print(f"❌ Could not find {routes_file}")
        return

    with open(routes_file, "r", encoding="utf-8") as f:
        routes_content = f.read()

    # 1. Parse Fiber Groups (e.g., api := app.Group("/api", ...))
    groups = {"app": ""}
    group_pattern = re.compile(r'(\w+)\s*:=\s*(\w+)\.Group\(\s*"([^"]+)"')
    for match in group_pattern.finditer(routes_content):
        var_name, parent_var, prefix = match.groups()
        parent_path = groups.get(parent_var, "")
        groups[var_name] = parent_path + prefix

    # 2. Parse Routes (e.g., in.Get("/path/:id", pkg.Handler))
    route_pattern = re.compile(
        r'(\w+)\.(Get|Post|Put|Delete|Patch)\(\s*"([^"]+)"\s*,\s*(\w+)\.(\w+)'
    )
    handlers_map = {}

    for match in route_pattern.finditer(routes_content):
        group_var, method, sub_path, pkg, func_name = match.groups()
        base_path = groups.get(group_var, "")
        full_path = base_path + sub_path
        method = method.upper()

        key = (pkg, func_name)
        if key not in handlers_map:
            handlers_map[key] = []
        handlers_map[key].append((method, full_path))

    # 3. Scan all .go files in internal/
    go_files = []
    for root, dirs, files in os.walk("internal"):
        for file in files:
            if file.endswith(".go") and not file.endswith("_test.go"):
                go_files.append(os.path.join(root, file))

    modified_count = 0

    for filepath in go_files:
        with open(filepath, "r", encoding="utf-8") as f:
            lines = f.readlines()

        # Get package name
        pkg_name = ""
        for line in lines:
            m = re.match(r"^package\s+(\w+)", line)
            if m:
                pkg_name = m.group(1)
                break

        if not pkg_name:
            continue

        new_lines = []
        changed = False
        i = 0

        while i < len(lines):
            line = lines[i]
            func_match = re.match(r"^func\s+([A-Za-z0-9_]+)\s*\(", line)

            if func_match:
                func_name = func_match.group(1)
                key = (pkg_name, func_name)

                if key in handlers_map:
                    # Check if already annotated (look back up to 20 lines)
                    already_annotated = False
                    for j in range(max(0, len(new_lines) - 20), len(new_lines)):
                        if "@Router" in new_lines[j] or "@Summary" in new_lines[j]:
                            already_annotated = True
                            break

                    if not already_annotated:
                        routes = handlers_map[key]
                        method, full_path = routes[0]

                        # Convert :id to {id} for Swagger
                        swagger_path = re.sub(r":(\w+)", r"{\1}", full_path)
                        params = re.findall(r":(\w+)", full_path)

                        annotations = []
                        annotations.append(f"// {func_name} godoc\n")
                        annotations.append(f"// @Summary {method} {full_path}\n")
                        annotations.append(f"// @Tags {pkg_name}\n")
                        annotations.append(f"// @Produce json\n")

                        # Add Security if it's a protected route
                        if "/api/in" in full_path:
                            annotations.append(f"// @Security BearerAuth\n")
                            annotations.append(f"// @Security SessionId\n")

                        # Add Path Params
                        for p in params:
                            annotations.append(
                                f'// @Param {p} path string true "{p}"\n'
                            )

                        # Smart Query Params based on your existing logic
                        if (
                            "/posts" in full_path
                            and method == "GET"
                            and not full_path.endswith("/results")
                        ):
                            annotations.append(
                                f'// @Param workspace_id query string true "Workspace ID"\n'
                            )
                        if full_path.endswith("/results") and method == "GET":
                            annotations.append(
                                f'// @Param attempt_id query string true "Publish attempt ID"\n'
                            )

                        annotations.append(
                            f"// @Success 200 {{object}} map[string]any\n"
                        )
                        annotations.append(
                            f"// @Failure 400 {{object}} map[string]string\n"
                        )

                        # Add all routers (in case one handler serves multiple paths)
                        for r_method, r_path in routes:
                            r_swagger_path = re.sub(r":(\w+)", r"{\1}", r_path)
                            annotations.append(
                                f"// @Router {r_swagger_path} [{r_method.lower()}]\n"
                            )

                        new_lines.extend(annotations)
                        changed = True

            new_lines.append(line)
            i += 1

        if changed:
            with open(filepath, "w", encoding="utf-8") as f:
                f.writelines(new_lines)
            modified_count += 1
            print(f"✅ Annotated: {filepath}")

    print(f"\n🎉 Done! Injected Swagger comments into {modified_count} files.")
    print(
        "\n👉 Next step: Run `swag init -g cmd/api/main.go --parseDependency --parseInternal -o swagger_docs`"
    )


if __name__ == "__main__":
    main()
