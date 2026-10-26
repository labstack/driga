import {MantineProvider} from "@mantine/core";
import {renderToStaticMarkup} from "react-dom/server";
import {expect,it} from "vitest";
import {RowMenu,rowProps} from "./RowMenu";
it("exposes keyboard row actions through a named button",()=>{const row={id:7,title:"Review API"};const props=rowProps(["jump","stop"],row,async()=>{});const html=renderToStaticMarkup(<MantineProvider><table><tbody><tr {...props}><td>Review API</td><td><RowMenu kinds={["jump","stop"]} row={row} act={async()=>{}} label="Review API"/></td></tr></tbody></table></MantineProvider>);expect(html).toContain('aria-label="Actions for Review API"');expect(html).not.toMatch(/<tr[^>]*tabindex/);});
