// @vitest-environment happy-dom
import { MantineProvider } from "@mantine/core";
import { renderToStaticMarkup } from "react-dom/server";
import { expect,it } from "vitest";
import { PanelView } from "./PanelView";
it("renders untrusted panel Markdown without HTML execution or image requests",()=>{
 const html=renderToStaticMarkup(<MantineProvider><PanelView panel={{id:"text",title:"Text",viz:"text",content:'<script>alert(1)</script>\n![exfil](https://attacker.example/beacon)\n[unsafe](javascript:alert(1))'}}/></MantineProvider>);
 expect(html).not.toMatch(/<script|<img|href="javascript:/);expect(html).not.toContain('src="https://attacker');
});
